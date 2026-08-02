// File: contrib/ops/fepmesh/main.go
//
// Process-level FEP-ae0c repeated-ID/reflection diagnostic. The command starts
// two Activity-Relay API processes, two workers, separate Redis containers,
// local trusted TLS frontends, and a signed ActivityPub origin. It emits a
// classification report; successful execution is not itself a passing loop
// invariant.

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-fed/httpsig"
	"github.com/redis/go-redis/v9"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

const (
	seedHeader       = "X-Activity-Relay-FEP-Seed"
	controlHeader    = "X-Activity-Relay-FEP-Control"
	publicCollection = "https://www.w3.org/ns/activitystreams#Public"
)

type postEvent struct {
	At             string `json:"at"`
	Destination    string `json:"destination"`
	ActivityID     string `json:"activity_id,omitempty"`
	ActivityType   string `json:"activity_type,omitempty"`
	Actor          string `json:"actor,omitempty"`
	ObjectID       string `json:"object_id,omitempty"`
	BodySHA256     string `json:"body_sha256"`
	SignatureKeyID string `json:"signature_key_id,omitempty"`
	SignatureValid bool   `json:"signature_valid"`
	Seed           bool   `json:"seed"`
	Forwarded      bool   `json:"forwarded"`
	ResponseStatus int    `json:"response_status"`
}

type getEvent struct {
	At             string `json:"at"`
	Destination    string `json:"destination"`
	Path           string `json:"path"`
	SignatureKeyID string `json:"signature_key_id,omitempty"`
	SignatureValid bool   `json:"signature_valid"`
	Control        bool   `json:"control"`
	ResponseStatus int    `json:"response_status"`
}

type redisKeyState struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Category string `json:"category"`
}

type redisState struct {
	At         string           `json:"at"`
	Relay      string           `json:"relay"`
	Categories map[string]int64 `json:"categories"`
	Keys       []redisKeyState  `json:"keys"`
}

type processRecord struct {
	Name       string `json:"name"`
	PID        int    `json:"pid"`
	ConfigPath string `json:"config_path"`
	LogPath    string `json:"log_path"`
}

type report struct {
	SchemaVersion               int             `json:"schema_version"`
	BaselineBinarySHA256        string          `json:"baseline_binary_sha256"`
	Classification              string          `json:"classification"`
	InfrastructureError         string          `json:"infrastructure_error,omitempty"`
	StartedAt                   string          `json:"started_at"`
	FinishedAt                  string          `json:"finished_at"`
	ObservationTimeout          string          `json:"observation_timeout"`
	QuietWindow                 string          `json:"quiet_window"`
	ReflectionThreshold         int64           `json:"reflection_threshold"`
	SeedStatus                  int             `json:"seed_status"`
	GeneratedCrossRelayPosts    int64           `json:"generated_cross_relay_posts"`
	UniqueGeneratedActivities   int             `json:"unique_generated_activity_ids"`
	RepeatedIdenticalDeliveries int             `json:"repeated_identical_deliveries"`
	NewRelayAnnounces           int             `json:"new_relay_announce_ids"`
	SignedGETs                  int             `json:"signed_gets"`
	InvalidSignedGETs           int             `json:"invalid_signed_gets"`
	InvalidSignedPOSTs          int             `json:"invalid_signed_posts"`
	RelayAActor                 string          `json:"relay_a_actor"`
	RelayBActor                 string          `json:"relay_b_actor"`
	OriginActor                 string          `json:"origin_actor"`
	OriginActivity              string          `json:"origin_activity"`
	Posts                       []postEvent     `json:"posts"`
	GETs                        []getEvent      `json:"gets"`
	Redis                       []redisState    `json:"redis"`
	Processes                   []processRecord `json:"processes"`
	Notes                       []string        `json:"notes"`
}

type collector struct {
	mu    sync.Mutex
	posts []postEvent
	gets  []getEvent
}

func (c *collector) addPost(event postEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.posts = append(c.posts, event)
}

func (c *collector) addGET(event getEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets = append(c.gets, event)
}

func (c *collector) snapshot() ([]postEvent, []getEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	posts := append([]postEvent(nil), c.posts...)
	gets := append([]getEvent(nil), c.gets...)
	return posts, gets
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(body)
}

type processSet struct {
	cancel context.CancelFunc
	cmds   []*exec.Cmd
	files  []*os.File
}

func (set *processSet) close() {
	if set.cancel != nil {
		set.cancel()
	}
	for _, command := range set.cmds {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
	for _, command := range set.cmds {
		if command.Process != nil {
			_, _ = command.Process.Wait()
		}
	}
	for _, file := range set.files {
		_ = file.Close()
	}
}

type redisInstance struct {
	name   string
	port   int
	client *redis.Client
}

func runOutput(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"%s %s: %w: %s",
			name,
			strings.Join(args, " "),
			err,
			strings.TrimSpace(string(output)),
		)
	}
	return strings.TrimSpace(string(output)), nil
}

func freePort(ip string) (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func startRedis(name string, port int) (*redisInstance, error) {
	_, err := runOutput(
		"docker", "run", "--detach", "--rm",
		"--name", name,
		"--publish", fmt.Sprintf("127.0.0.1:%d:6379", port),
		"redis:7-alpine",
	)
	if err != nil {
		return nil, err
	}
	instance := &redisInstance{
		name: name,
		port: port,
		client: redis.NewClient(&redis.Options{
			Addr: fmt.Sprintf("127.0.0.1:%d", port),
		}),
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = instance.client.Ping(ctx).Err()
		cancel()
		if err == nil {
			return instance, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	instance.close()
	return nil, errors.New("Redis did not become ready")
}

func (instance *redisInstance) close() {
	if instance == nil {
		return
	}
	if instance.client != nil {
		_ = instance.client.Close()
	}
	if instance.name != "" {
		_, _ = runOutput("docker", "rm", "-f", instance.name)
	}
}

func generateRSAKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

func writePrivateKey(path string, key *rsa.PrivateKey) error {
	data := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return os.WriteFile(path, data, 0o600)
}

func publicKeyPEM(key *rsa.PublicKey) (string, error) {
	data, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: data,
	})), nil
}

func keyFingerprint(key *rsa.PublicKey) (string, error) {
	data, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func generateCA() (*x509.Certificate, *rsa.PrivateKey, []byte, error) {
	key, err := generateRSAKey()
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Activity-Relay FEP mesh CA"},
		NotBefore:             now,
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(
		rand.Reader, template, template, &key.PublicKey, key,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	}), nil
}

func generateLeaf(
	ca *x509.Certificate,
	caKey *rsa.PrivateKey,
	ip net.IP,
	serial int64,
) (tls.Certificate, error) {
	key, err := generateRSAKey()
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: ip.String()},
		NotBefore:    now,
		NotAfter:     now.Add(24 * time.Hour),
		IPAddresses:  []net.IP{ip},
		KeyUsage: x509.KeyUsageDigitalSignature |
			x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(
		rand.Reader, template, ca, &key.PublicKey, caKey,
	)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	})
	privatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return tls.X509KeyPair(certificatePEM, privatePEM)
}

func startTLSServer(
	address string,
	certificate tls.Certificate,
	handler http.Handler,
) (*http.Server, net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, err
	}
	tlsListener := tls.NewListener(listener, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	})
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		_ = server.Serve(tlsListener)
	}()
	return server, listener, nil
}

func requestSignature(
	request *http.Request,
	keys map[string]*rsa.PublicKey,
) (string, bool) {
	if request.Header.Get("Signature") == "" {
		return "", false
	}
	request.Header.Set("Host", request.Host)
	verifier, err := httpsig.NewVerifier(request)
	if err != nil {
		return "", false
	}
	keyID := verifier.KeyId()
	publicKey := keys[keyID]
	if publicKey == nil {
		return keyID, false
	}
	if err := verifier.Verify(publicKey, httpsig.RSA_SHA256); err != nil {
		return keyID, false
	}
	return keyID, true
}

func activitySummary(body []byte) (string, string, string, string) {
	var activity map[string]interface{}
	if err := json.Unmarshal(body, &activity); err != nil {
		return "", "", "", ""
	}
	id, _ := activity["id"].(string)
	activityType, _ := activity["type"].(string)
	actor, _ := activity["actor"].(string)
	objectID := ""
	switch object := activity["object"].(type) {
	case string:
		objectID = object
	case map[string]interface{}:
		objectID, _ = object["id"].(string)
	}
	return id, activityType, actor, objectID
}

func startRelayProxy(
	name string,
	publicAddress string,
	certificate tls.Certificate,
	backendAddress string,
	keys map[string]*rsa.PublicKey,
	observations *collector,
	generated *atomic.Int64,
	threshold int64,
	thresholdReached *atomic.Bool,
) (*http.Server, net.Listener, error) {
	target, err := url.Parse("http://" + backendAddress)
	if err != nil {
		return nil, nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: nil}
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		incomingHost := request.Host
		originalDirector(request)
		request.Host = incomingHost
	}

	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(controlHeader) == "1" {
			proxy.ServeHTTP(writer, request)
			return
		}

		if request.Method == http.MethodGet {
			keyID, valid := requestSignature(request, keys)
			status := &statusWriter{ResponseWriter: writer}
			proxy.ServeHTTP(status, request)
			observations.addGET(getEvent{
				At:             time.Now().UTC().Format(time.RFC3339Nano),
				Destination:    name,
				Path:           request.URL.Path,
				SignatureKeyID: keyID,
				SignatureValid: valid,
				ResponseStatus: status.status,
			})
			return
		}

		if request.Method != http.MethodPost || request.URL.Path != "/inbox" {
			proxy.ServeHTTP(writer, request)
			return
		}

		body, err := io.ReadAll(io.LimitReader(request.Body, 4<<20))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))

		keyID, valid := requestSignature(request, keys)
		activityID, activityType, actor, objectID := activitySummary(body)
		sum := sha256.Sum256(body)
		seed := request.Header.Get(seedHeader) == "1"
		event := postEvent{
			At:             time.Now().UTC().Format(time.RFC3339Nano),
			Destination:    name,
			ActivityID:     activityID,
			ActivityType:   activityType,
			Actor:          actor,
			ObjectID:       objectID,
			BodySHA256:     hex.EncodeToString(sum[:]),
			SignatureKeyID: keyID,
			SignatureValid: valid,
			Seed:           seed,
		}

		if !seed {
			count := generated.Add(1)
			if count >= threshold {
				thresholdReached.Store(true)
				event.Forwarded = false
				event.ResponseStatus = http.StatusAccepted
				observations.addPost(event)
				writer.WriteHeader(http.StatusAccepted)
				return
			}
		}

		status := &statusWriter{ResponseWriter: writer}
		proxy.ServeHTTP(status, request)
		event.Forwarded = true
		event.ResponseStatus = status.status
		observations.addPost(event)
	})
	return startTLSServer(publicAddress, certificate, handler)
}

func writeConfig(
	path string,
	keyPath string,
	redisPort int,
	apiAddress string,
	publicAuthority string,
	serviceName string,
) error {
	content := fmt.Sprintf(
		`ACTOR_PEM: %s
REDIS_URL: redis://127.0.0.1:%d
RELAY_BIND: %s
RELAY_DOMAIN: %s
RELAY_SERVICENAME: %s
JOB_CONCURRENCY: 1
MAX_ACTIVITY_BYTES: 1048576
MAX_FANOUT_TARGETS: 100
MAX_QUEUE_JOBS: 1000
`,
		keyPath,
		redisPort,
		apiAddress,
		publicAuthority,
		serviceName,
	)
	return os.WriteFile(path, []byte(content), 0o600)
}

func startRelayProcess(
	ctx context.Context,
	binary string,
	configPath string,
	mode string,
	caPath string,
	logPath string,
) (*exec.Cmd, *os.File, error) {
	logFile, err := os.OpenFile(
		logPath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		return nil, nil, err
	}
	command := exec.CommandContext(
		ctx,
		binary,
		"--config", configPath,
		mode,
	)
	command.Env = append(os.Environ(),
		"SSL_CERT_FILE="+caPath,
		"NO_PROXY=127.0.0.1,127.0.0.2,127.0.0.3,127.0.0.4,localhost",
		"no_proxy=127.0.0.1,127.0.0.2,127.0.0.3,127.0.0.4,localhost",
		"HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=",
		"http_proxy=", "https_proxy=", "all_proxy=",
	)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return nil, nil, err
	}
	return command, logFile, nil
}

func waitForActor(client *http.Client, actorURL string) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodGet, actorURL, nil)
		if err != nil {
			return err
		}
		request.Header.Set(controlHeader, "1")
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("actor endpoint did not become ready: %s", actorURL)
}

func seedAnnounce(
	client *http.Client,
	inboxURL string,
	relayAActor string,
	relayBActor string,
	relayBKey *rsa.PrivateKey,
	originActivity string,
) (int, error) {
	activity := map[string]interface{}{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       relayBActor + "/activities/fep-ae0c-seed",
		"type":     "Announce",
		"actor":    relayBActor,
		"object":   originActivity,
		"to":       []string{relayAActor},
	}
	body, err := json.Marshal(activity)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(
		http.MethodPost,
		inboxURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/activity+json")
	request.Header.Set(seedHeader, "1")
	signer, err := relayhttpsig.NewSigner(
		relayBActor+"#main-key",
		relayBKey,
	)
	if err != nil {
		return 0, err
	}
	if err := signer.SignPOST(request, body); err != nil {
		return 0, err
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusAccepted {
		return response.StatusCode, fmt.Errorf(
			"seed POST returned %s: %s",
			response.Status,
			strings.TrimSpace(string(responseBody)),
		)
	}
	return response.StatusCode, nil
}

func classifyKey(key string) string {
	lower := strings.ToLower(key)
	switch {
	case key == "relay":
		return "ready"
	case strings.Contains(lower, "delayed"):
		return "delayed"
	case strings.Contains(lower, "claim") || strings.Contains(lower, "reserved"):
		return "claimed_or_reserved"
	case strings.Contains(lower, "retry"):
		return "retry"
	case strings.HasPrefix(key, "relay:activity:"):
		return "retained_activity"
	case strings.HasPrefix(key, "relay:canonical:"):
		return "canonical_marker"
	default:
		return "other"
	}
}

func redisSnapshot(
	ctx context.Context,
	name string,
	client *redis.Client,
) (redisState, error) {
	state := redisState{
		At:         time.Now().UTC().Format(time.RFC3339Nano),
		Relay:      name,
		Categories: make(map[string]int64),
	}
	keys := make([]string, 0)
	iterator := client.Scan(ctx, 0, "*", 256).Iterator()
	for iterator.Next(ctx) {
		keys = append(keys, iterator.Val())
	}
	if err := iterator.Err(); err != nil {
		return state, err
	}
	sort.Strings(keys)
	for _, key := range keys {
		kind, err := client.Type(ctx, key).Result()
		if err != nil {
			return state, err
		}
		var size int64
		switch kind {
		case "string":
			size, _ = client.StrLen(ctx, key).Result()
		case "list":
			size, _ = client.LLen(ctx, key).Result()
		case "set":
			size, _ = client.SCard(ctx, key).Result()
		case "zset":
			size, _ = client.ZCard(ctx, key).Result()
		case "hash":
			size, _ = client.HLen(ctx, key).Result()
		case "stream":
			size, _ = client.XLen(ctx, key).Result()
		}
		category := classifyKey(key)
		state.Categories[category] += size
		state.Keys = append(state.Keys, redisKeyState{
			Key:      key,
			Type:     kind,
			Size:     size,
			Category: category,
		})
	}
	return state, nil
}

func summarizePosts(posts []postEvent) (int, int, int, int) {
	uniqueIDs := make(map[string]struct{})
	uniqueDeliveries := make(map[string]int)
	announceIDs := make(map[string]struct{})
	invalid := 0
	for _, event := range posts {
		if event.Seed {
			continue
		}
		if event.ActivityID != "" {
			uniqueIDs[event.ActivityID] = struct{}{}
		}
		key := event.Destination + "\x00" + event.ActivityID + "\x00" + event.BodySHA256
		uniqueDeliveries[key]++
		if event.ActivityType == "Announce" && event.ActivityID != "" {
			announceIDs[event.ActivityID] = struct{}{}
		}
		if !event.SignatureValid {
			invalid++
		}
	}
	repeated := 0
	for _, count := range uniqueDeliveries {
		if count > 1 {
			repeated += count - 1
		}
	}
	return len(uniqueIDs), repeated, len(announceIDs), invalid
}

func writeJSON(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

type probeFailure struct {
	err error
}

func (failure probeFailure) Error() string {
	return failure.err.Error()
}

func main() {
	os.Exit(runProbe())
}

func runProbe() (exitCode int) {
	var relayBinary string
	var evidenceDir string
	var timeout time.Duration
	var quietWindow time.Duration
	var threshold int64

	flag.StringVar(&relayBinary, "relay-binary", "", "path to Activity-Relay binary")
	flag.StringVar(&evidenceDir, "evidence-dir", "", "directory for private evidence")
	flag.DurationVar(&timeout, "timeout", 12*time.Second, "maximum observation time")
	flag.DurationVar(&quietWindow, "quiet-window", 2*time.Second, "settled-count window")
	flag.Int64Var(&threshold, "reflection-threshold", 12, "hard generated POST limit")
	flag.Parse()

	if relayBinary == "" || evidenceDir == "" {
		fmt.Fprintln(os.Stderr, "--relay-binary and --evidence-dir are required")
		os.Exit(2)
	}
	if threshold < 3 || timeout <= quietWindow {
		fmt.Fprintln(os.Stderr, "invalid threshold or observation timing")
		os.Exit(2)
	}
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runtimeDir := filepath.Join(evidenceDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(runtimeDir)

	started := time.Now()
	result := report{
		SchemaVersion:       1,
		StartedAt:           started.UTC().Format(time.RFC3339Nano),
		ObservationTimeout:  timeout.String(),
		QuietWindow:         quietWindow.String(),
		ReflectionThreshold: threshold,
		Notes: []string{
			"A zero process exit only means the probe infrastructure completed.",
			"Only no_reflection_observed or an explicitly bounded reflection_settled result can later become a passing invariant.",
			"The hard threshold returns HTTP 202 without forwarding the threshold-reaching POST.",
		},
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			var err error
			switch value := recovered.(type) {
			case probeFailure:
				err = value.err
			case error:
				err = value
			default:
				err = fmt.Errorf("unexpected panic: %v", value)
			}
			result.Classification = "infrastructure_failure"
			result.InfrastructureError = err.Error()
			result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			_ = writeJSON(filepath.Join(evidenceDir, "report.json"), result)
			fmt.Fprintln(os.Stderr, err)
			exitCode = 1
		}
	}()
	fail := func(err error) {
		panic(probeFailure{err: err})
	}

	binaryHash, err := fileSHA256(relayBinary)
	if err != nil {
		fail(err)
	}
	result.BaselineBinarySHA256 = binaryHash

	suffix := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	redisAPort, err := freePort("127.0.0.1")
	if err != nil {
		fail(err)
	}
	redisBPort, err := freePort("127.0.0.1")
	if err != nil {
		fail(err)
	}
	redisA, err := startRedis("activity-relay-fepmesh-a-"+suffix, redisAPort)
	if err != nil {
		fail(err)
	}
	defer redisA.close()
	redisB, err := startRedis("activity-relay-fepmesh-b-"+suffix, redisBPort)
	if err != nil {
		fail(err)
	}
	defer redisB.close()

	apiAPort, err := freePort("127.0.0.1")
	if err != nil {
		fail(err)
	}
	apiBPort, err := freePort("127.0.0.1")
	if err != nil {
		fail(err)
	}
	tlsAPort, err := freePort("127.0.0.2")
	if err != nil {
		fail(err)
	}
	tlsBPort, err := freePort("127.0.0.3")
	if err != nil {
		fail(err)
	}
	originPort, err := freePort("127.0.0.4")
	if err != nil {
		fail(err)
	}

	apiAAddress := fmt.Sprintf("127.0.0.1:%d", apiAPort)
	apiBAddress := fmt.Sprintf("127.0.0.1:%d", apiBPort)
	publicAAddress := fmt.Sprintf("127.0.0.2:%d", tlsAPort)
	publicBAddress := fmt.Sprintf("127.0.0.3:%d", tlsBPort)
	originAddress := fmt.Sprintf("127.0.0.4:%d", originPort)
	result.RelayAActor = "https://" + publicAAddress + "/actor"
	result.RelayBActor = "https://" + publicBAddress + "/actor"
	result.OriginActor = "https://" + originAddress + "/actor"
	result.OriginActivity = "https://" + originAddress + "/activities/create-1"

	relayAKey, err := generateRSAKey()
	if err != nil {
		fail(err)
	}
	relayBKey, err := generateRSAKey()
	if err != nil {
		fail(err)
	}
	originKey, err := generateRSAKey()
	if err != nil {
		fail(err)
	}
	relayAKeyPath := filepath.Join(runtimeDir, "relay-a.pem")
	relayBKeyPath := filepath.Join(runtimeDir, "relay-b.pem")
	if err := writePrivateKey(relayAKeyPath, relayAKey); err != nil {
		fail(err)
	}
	if err := writePrivateKey(relayBKeyPath, relayBKey); err != nil {
		fail(err)
	}

	ca, caKey, caPEM, err := generateCA()
	if err != nil {
		fail(err)
	}
	caPath := filepath.Join(evidenceDir, "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		fail(err)
	}
	certA, err := generateLeaf(ca, caKey, net.ParseIP("127.0.0.2"), 2)
	if err != nil {
		fail(err)
	}
	certB, err := generateLeaf(ca, caKey, net.ParseIP("127.0.0.3"), 3)
	if err != nil {
		fail(err)
	}
	certOrigin, err := generateLeaf(ca, caKey, net.ParseIP("127.0.0.4"), 4)
	if err != nil {
		fail(err)
	}

	fingerprints := make(map[string]string)
	fingerprints[result.RelayAActor+"#main-key"], err = keyFingerprint(&relayAKey.PublicKey)
	if err != nil {
		fail(err)
	}
	fingerprints[result.RelayBActor+"#main-key"], err = keyFingerprint(&relayBKey.PublicKey)
	if err != nil {
		fail(err)
	}
	fingerprints[result.OriginActor+"#main-key"], err = keyFingerprint(&originKey.PublicKey)
	if err != nil {
		fail(err)
	}
	if err := writeJSON(filepath.Join(evidenceDir, "public-key-fingerprints.json"), fingerprints); err != nil {
		fail(err)
	}

	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM(caPEM) {
		fail(errors.New("unable to load generated CA"))
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    rootPool,
			},
		},
	}

	keys := map[string]*rsa.PublicKey{
		result.RelayAActor + "#main-key": &relayAKey.PublicKey,
		result.RelayBActor + "#main-key": &relayBKey.PublicKey,
	}
	observations := &collector{}
	var generated atomic.Int64
	var thresholdReached atomic.Bool

	proxyA, listenerA, err := startRelayProxy(
		"relay-a", publicAAddress, certA, apiAAddress, keys,
		observations, &generated, threshold, &thresholdReached,
	)
	if err != nil {
		fail(err)
	}
	defer func() {
		_ = proxyA.Close()
		_ = listenerA.Close()
	}()
	proxyB, listenerB, err := startRelayProxy(
		"relay-b", publicBAddress, certB, apiBAddress, keys,
		observations, &generated, threshold, &thresholdReached,
	)
	if err != nil {
		fail(err)
	}
	defer func() {
		_ = proxyB.Close()
		_ = listenerB.Close()
	}()

	originPublicKey, err := publicKeyPEM(&originKey.PublicKey)
	if err != nil {
		fail(err)
	}
	originActor := map[string]interface{}{
		"@context":          "https://www.w3.org/ns/activitystreams",
		"id":                result.OriginActor,
		"type":              "Person",
		"preferredUsername": "origin",
		"inbox":             "https://" + originAddress + "/inbox",
		"endpoints": map[string]interface{}{
			"sharedInbox": "https://" + originAddress + "/inbox",
		},
		"publicKey": map[string]interface{}{
			"id":           result.OriginActor + "#main-key",
			"owner":        result.OriginActor,
			"publicKeyPem": originPublicKey,
		},
	}
	originActivity := map[string]interface{}{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       result.OriginActivity,
		"type":     "Create",
		"actor":    result.OriginActor,
		"object": map[string]interface{}{
			"id":           "https://" + originAddress + "/objects/1",
			"type":         "Note",
			"attributedTo": result.OriginActor,
			"content":      "FEP-ae0c two-relay reflection probe",
		},
		"to": []string{publicCollection},
	}
	originHandler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		keyID, valid := requestSignature(request, keys)
		event := getEvent{
			At:             time.Now().UTC().Format(time.RFC3339Nano),
			Destination:    "origin",
			Path:           request.URL.Path,
			SignatureKeyID: keyID,
			SignatureValid: valid,
		}
		if !valid {
			event.ResponseStatus = http.StatusUnauthorized
			observations.addGET(event)
			http.Error(writer, "invalid HTTP signature", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/activity+json")
		switch request.URL.Path {
		case "/actor":
			event.ResponseStatus = http.StatusOK
			observations.addGET(event)
			_ = json.NewEncoder(writer).Encode(originActor)
		case "/activities/create-1":
			event.ResponseStatus = http.StatusOK
			observations.addGET(event)
			_ = json.NewEncoder(writer).Encode(originActivity)
		default:
			event.ResponseStatus = http.StatusNotFound
			observations.addGET(event)
			http.NotFound(writer, request)
		}
	})
	originServer, originListener, err := startTLSServer(originAddress, certOrigin, originHandler)
	if err != nil {
		fail(err)
	}
	defer func() {
		_ = originServer.Close()
		_ = originListener.Close()
	}()

	ctx := context.Background()
	if err := redisA.client.HSet(ctx, "relay:follower:127.0.0.3", map[string]interface{}{
		"inbox_url":       "https://" + publicBAddress + "/inbox",
		"activity_id":     result.RelayBActor + "/activities/follow-a",
		"actor_id":        result.RelayBActor,
		"mutually_follow": "1",
	}).Err(); err != nil {
		fail(err)
	}
	if err := redisB.client.HSet(ctx, "relay:follower:127.0.0.2", map[string]interface{}{
		"inbox_url":       "https://" + publicAAddress + "/inbox",
		"activity_id":     result.RelayAActor + "/activities/follow-b",
		"actor_id":        result.RelayAActor,
		"mutually_follow": "1",
	}).Err(); err != nil {
		fail(err)
	}

	configA := filepath.Join(evidenceDir, "relay-a.yml")
	configB := filepath.Join(evidenceDir, "relay-b.yml")
	if err := writeConfig(configA, relayAKeyPath, redisAPort, apiAAddress, publicAAddress, "FEP Mesh Relay A"); err != nil {
		fail(err)
	}
	if err := writeConfig(configB, relayBKeyPath, redisBPort, apiBAddress, publicBAddress, "FEP Mesh Relay B"); err != nil {
		fail(err)
	}

	processContext, cancelProcesses := context.WithCancel(context.Background())
	processes := &processSet{cancel: cancelProcesses}
	defer processes.close()
	for _, specification := range []struct {
		name   string
		config string
		mode   string
		log    string
	}{
		{"relay-a-server", configA, "server", "relay-a-server.log"},
		{"relay-a-worker", configA, "worker", "relay-a-worker.log"},
		{"relay-b-server", configB, "server", "relay-b-server.log"},
		{"relay-b-worker", configB, "worker", "relay-b-worker.log"},
	} {
		logPath := filepath.Join(evidenceDir, specification.log)
		command, logFile, startErr := startRelayProcess(
			processContext, relayBinary, specification.config,
			specification.mode, caPath, logPath,
		)
		if startErr != nil {
			fail(startErr)
		}
		processes.cmds = append(processes.cmds, command)
		processes.files = append(processes.files, logFile)
		result.Processes = append(result.Processes, processRecord{
			Name:       specification.name,
			PID:        command.Process.Pid,
			ConfigPath: filepath.Base(specification.config),
			LogPath:    filepath.Base(logPath),
		})
	}

	if err := waitForActor(client, result.RelayAActor); err != nil {
		fail(err)
	}
	if err := waitForActor(client, result.RelayBActor); err != nil {
		fail(err)
	}
	initialA, err := redisSnapshot(ctx, "relay-a-initial", redisA.client)
	if err != nil {
		fail(err)
	}
	initialB, err := redisSnapshot(ctx, "relay-b-initial", redisB.client)
	if err != nil {
		fail(err)
	}
	result.Redis = append(result.Redis, initialA, initialB)

	seedStatus, err := seedAnnounce(
		client,
		"https://"+publicAAddress+"/inbox",
		result.RelayAActor,
		result.RelayBActor,
		relayBKey,
		result.OriginActivity,
	)
	result.SeedStatus = seedStatus
	if err != nil {
		fail(err)
	}

	classification := ""
	lastCount := generated.Load()
	stableSince := time.Now()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if thresholdReached.Load() {
			classification = "reflection_threshold_reached"
			break
		}
		current := generated.Load()
		if current != lastCount {
			lastCount = current
			stableSince = time.Now()
		} else if time.Since(stableSince) >= quietWindow {
			if current == 0 {
				classification = "no_reflection_observed"
			} else {
				classification = "reflection_settled"
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if classification == "" {
		classification = "reflection_active_at_timeout"
	}

	time.Sleep(750 * time.Millisecond)
	finalA, err := redisSnapshot(ctx, "relay-a-final", redisA.client)
	if err != nil {
		fail(err)
	}
	finalB, err := redisSnapshot(ctx, "relay-b-final", redisB.client)
	if err != nil {
		fail(err)
	}
	result.Redis = append(result.Redis, finalA, finalB)
	posts, gets := observations.snapshot()
	result.Posts = posts
	result.GETs = gets
	result.GeneratedCrossRelayPosts = generated.Load()
	result.UniqueGeneratedActivities, result.RepeatedIdenticalDeliveries, result.NewRelayAnnounces, result.InvalidSignedPOSTs = summarizePosts(posts)
	for _, event := range gets {
		if event.Control {
			continue
		}
		if event.SignatureValid {
			result.SignedGETs++
		} else {
			result.InvalidSignedGETs++
		}
	}
	if result.InvalidSignedPOSTs > 0 || result.InvalidSignedGETs > 0 {
		fail(fmt.Errorf(
			"signature evidence contains %d invalid POSTs and %d invalid GETs",
			result.InvalidSignedPOSTs,
			result.InvalidSignedGETs,
		))
	}
	result.Classification = classification
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)

	if err := writeJSON(filepath.Join(evidenceDir, "report.json"), result); err != nil {
		fail(err)
	}
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(output))
	return 0
}

// EOF: contrib/ops/fepmesh/main.go
