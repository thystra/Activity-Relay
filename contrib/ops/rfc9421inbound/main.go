// File: contrib/ops/rfc9421inbound/main.go
//
// Real-process RFC 9421 inbound acceptance and replay probe.

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
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
	"strings"
	"sync/atomic"
	"time"

	legacyhttpsig "github.com/go-fed/httpsig"
	"github.com/redis/go-redis/v9"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

type report struct {
	SchemaVersion          int               `json:"schema_version"`
	Classification         string            `json:"classification"`
	InfrastructureError    string            `json:"infrastructure_error,omitempty"`
	StartedAt              string            `json:"started_at"`
	FinishedAt             string            `json:"finished_at"`
	RelayActor             string            `json:"relay_actor"`
	OriginActor            string            `json:"origin_actor"`
	ValidStatus            int               `json:"valid_status"`
	ReplayStatus           int               `json:"replay_status"`
	TamperedStatus         int               `json:"tampered_status"`
	ValidAfterTamperStatus int               `json:"valid_after_tamper_status"`
	SignedActorGETs        int64             `json:"signed_actor_gets"`
	InvalidActorGETs       int64             `json:"invalid_actor_gets"`
	NonceMarkerCount       int               `json:"nonce_marker_count"`
	Metrics                map[string]string `json:"metrics"`
	ProcessPID             int               `json:"process_pid"`
	ConfigPath             string            `json:"config_path"`
	LogPath                string            `json:"log_path"`
}

type processState struct {
	cancel context.CancelFunc
	cmd    *exec.Cmd
	log    *os.File
}

func (state *processState) close() {
	if state == nil {
		return
	}
	if state.cancel != nil {
		state.cancel()
	}
	if state.cmd != nil && state.cmd.Process != nil {
		_ = state.cmd.Process.Kill()
		_, _ = state.cmd.Process.Wait()
	}
	if state.log != nil {
		_ = state.log.Close()
	}
}

func freePort(ip string) (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func generateKey() (*rsa.PrivateKey, error) {
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

func generateCA() (
	*x509.Certificate,
	*rsa.PrivateKey,
	[]byte,
	error,
) {
	key, err := generateKey()
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "RFC 9421 inbound probe CA"},
		NotBefore:             now,
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
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
	key, err := generateKey()
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
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		ca,
		&key.PublicKey,
		caKey,
	)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: der,
		}),
		pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		}),
	)
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

func startProxy(
	publicAddress string,
	certificate tls.Certificate,
	backendAddress string,
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
	return startTLSServer(publicAddress, certificate, proxy)
}

func startRedis(name string, port int) (*redis.Client, error) {
	command := exec.Command(
		"docker",
		"run",
		"--detach",
		"--rm",
		"--name",
		name,
		"--publish",
		fmt.Sprintf("127.0.0.1:%d:6379", port),
		"redis:7-alpine",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"start Redis: %w: %s",
			err,
			strings.TrimSpace(string(output)),
		)
	}
	client := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("127.0.0.1:%d", port),
	})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			time.Second,
		)
		err = client.Ping(ctx).Err()
		cancel()
		if err == nil {
			return client, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = client.Close()
	_ = exec.Command("docker", "rm", "-f", name).Run()
	return nil, errors.New("Redis did not become ready")
}

func verifyLegacyGET(
	request *http.Request,
	keyID string,
	publicKey *rsa.PublicKey,
) bool {
	request.Header.Set("Host", request.Host)
	verifier, err := legacyhttpsig.NewVerifier(request)
	if err != nil || verifier.KeyId() != keyID {
		return false
	}
	return verifier.Verify(
		publicKey,
		legacyhttpsig.RSA_SHA256,
	) == nil
}

func writeConfig(
	path string,
	keyPath string,
	redisPort int,
	apiAddress string,
	publicAuthority string,
	observabilityAddress string,
) error {
	content := fmt.Sprintf(
		`ACTOR_PEM: %s
REDIS_URL: redis://127.0.0.1:%d
RELAY_BIND: %s
OBSERVABILITY_BIND: %s
RELAY_DOMAIN: %s
RELAY_SERVICENAME: RFC 9421 inbound probe
JOB_CONCURRENCY: 1
MAX_ACTIVITY_BYTES: 1048576
MAX_FANOUT_TARGETS: 100
MAX_QUEUE_JOBS: 1000
`,
		keyPath,
		redisPort,
		apiAddress,
		observabilityAddress,
		publicAuthority,
	)
	return os.WriteFile(path, []byte(content), 0o600)
}

func startRelay(
	parent context.Context,
	binary string,
	configPath string,
	caPath string,
	logPath string,
) (*processState, error) {
	ctx, cancel := context.WithCancel(parent)
	logFile, err := os.OpenFile(
		logPath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		cancel()
		return nil, err
	}
	command := exec.CommandContext(
		ctx,
		binary,
		"--config",
		configPath,
		"server",
	)
	command.Env = append(
		os.Environ(),
		"SSL_CERT_FILE="+caPath,
		"NO_PROXY=127.0.0.1,127.0.0.2,127.0.0.3,localhost",
		"no_proxy=127.0.0.1,127.0.0.2,127.0.0.3,localhost",
		"HTTP_PROXY=",
		"HTTPS_PROXY=",
		"ALL_PROXY=",
		"http_proxy=",
		"https_proxy=",
		"all_proxy=",
	)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		cancel()
		return nil, err
	}
	return &processState{
		cancel: cancel,
		cmd:    command,
		log:    logFile,
	}, nil
}

func waitForOK(client *http.Client, address string) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(address)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("endpoint did not become ready: %s", address)
}

func cloneSignedRequest(
	request *http.Request,
	body []byte,
) (*http.Request, error) {
	cloned, err := http.NewRequest(
		request.Method,
		request.URL.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	cloned.Host = request.Host
	cloned.Header = request.Header.Clone()
	return cloned, nil
}

func send(
	client *http.Client,
	request *http.Request,
) (int, string, error) {
	response, err := client.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return response.StatusCode, strings.TrimSpace(string(body)), nil
}

func signedActivityRequest(
	inboxURL string,
	actorURL string,
	key *rsa.PrivateKey,
	id string,
) (*http.Request, []byte, error) {
	activity := map[string]interface{}{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       actorURL + "/activities/" + id,
		"type":     "Like",
		"actor":    actorURL,
		"object":   "https://example.invalid/object",
		"to": []string{
			"https://www.w3.org/ns/activitystreams#Public",
		},
	}
	body, err := json.Marshal(activity)
	if err != nil {
		return nil, nil, err
	}
	request, err := http.NewRequest(
		http.MethodPost,
		inboxURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Content-Type", "application/activity+json")
	signer, err := relayhttpsig.NewSigner(
		actorURL+"#main-key",
		key,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := signer.SignPOSTWithProfile(
		request,
		body,
		relayhttpsig.ProfileRFC9421,
	); err != nil {
		return nil, nil, err
	}
	return request, body, nil
}

func run() (result report, err error) {
	var relayBinary string
	var evidenceDir string
	flag.StringVar(&relayBinary, "relay-binary", "", "relay binary")
	flag.StringVar(&evidenceDir, "evidence-dir", "", "private evidence directory")
	flag.Parse()

	result.SchemaVersion = 1
	result.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	result.Classification = "infrastructure_failure"

	defer func() {
		result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err != nil {
			result.InfrastructureError = err.Error()
		}
	}()

	if relayBinary == "" || evidenceDir == "" {
		return result, errors.New(
			"--relay-binary and --evidence-dir are required",
		)
	}
	if !filepath.IsAbs(evidenceDir) {
		return result, errors.New("evidence directory must be absolute")
	}
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		return result, err
	}

	workdir, err := os.MkdirTemp("", "activity-relay-rfc9421-inbound.")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(workdir)

	relayIP := net.ParseIP("127.0.0.2")
	originIP := net.ParseIP("127.0.0.3")
	relayPublicPort, err := freePort(relayIP.String())
	if err != nil {
		return result, err
	}
	originPort, err := freePort(originIP.String())
	if err != nil {
		return result, err
	}
	backendPort, err := freePort("127.0.0.1")
	if err != nil {
		return result, err
	}
	metricsPort, err := freePort("127.0.0.1")
	if err != nil {
		return result, err
	}
	redisPort, err := freePort("127.0.0.1")
	if err != nil {
		return result, err
	}

	ca, caKey, caPEM, err := generateCA()
	if err != nil {
		return result, err
	}
	relayCertificate, err := generateLeaf(
		ca,
		caKey,
		relayIP,
		2,
	)
	if err != nil {
		return result, err
	}
	originCertificate, err := generateLeaf(
		ca,
		caKey,
		originIP,
		3,
	)
	if err != nil {
		return result, err
	}
	caPath := filepath.Join(workdir, "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		return result, err
	}

	relayKey, err := generateKey()
	if err != nil {
		return result, err
	}
	originKey, err := generateKey()
	if err != nil {
		return result, err
	}
	relayKeyPath := filepath.Join(workdir, "relay.pem")
	if err := writePrivateKey(relayKeyPath, relayKey); err != nil {
		return result, err
	}

	relayAuthority := fmt.Sprintf(
		"%s:%d",
		relayIP.String(),
		relayPublicPort,
	)
	originAuthority := fmt.Sprintf(
		"%s:%d",
		originIP.String(),
		originPort,
	)
	relayActor := "https://" + relayAuthority + "/actor"
	originActor := "https://" + originAuthority + "/actor"
	result.RelayActor = relayActor
	result.OriginActor = originActor

	originPublicKey, err := publicKeyPEM(&originKey.PublicKey)
	if err != nil {
		return result, err
	}

	var signedGETs atomic.Int64
	var invalidGETs atomic.Int64
	originHandler := http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/actor" {
				http.NotFound(writer, request)
				return
			}
			if verifyLegacyGET(
				request,
				relayActor+"#main-key",
				&relayKey.PublicKey,
			) {
				signedGETs.Add(1)
			} else {
				invalidGETs.Add(1)
			}
			writer.Header().Set(
				"Content-Type",
				"application/activity+json",
			)
			_ = json.NewEncoder(writer).Encode(map[string]interface{}{
				"@context": []string{
					"https://www.w3.org/ns/activitystreams",
					"https://w3id.org/security/v1",
				},
				"id":    originActor,
				"type":  "Application",
				"inbox": "https://" + originAuthority + "/inbox",
				"endpoints": map[string]string{
					"sharedInbox": "https://" + originAuthority + "/inbox",
				},
				"publicKey": map[string]string{
					"id":           originActor + "#main-key",
					"owner":        originActor,
					"publicKeyPem": originPublicKey,
				},
			})
		},
	)
	originServer, originListener, err := startTLSServer(
		net.JoinHostPort(originIP.String(), fmt.Sprint(originPort)),
		originCertificate,
		originHandler,
	)
	if err != nil {
		return result, err
	}
	defer originServer.Close()
	defer originListener.Close()

	backendAddress := fmt.Sprintf("127.0.0.1:%d", backendPort)
	proxyServer, proxyListener, err := startProxy(
		net.JoinHostPort(relayIP.String(), fmt.Sprint(relayPublicPort)),
		relayCertificate,
		backendAddress,
	)
	if err != nil {
		return result, err
	}
	defer proxyServer.Close()
	defer proxyListener.Close()

	redisName := fmt.Sprintf(
		"activity-relay-rfc9421-inbound-%d",
		time.Now().UnixNano(),
	)
	redisClient, err := startRedis(redisName, redisPort)
	if err != nil {
		return result, err
	}
	defer func() {
		_ = redisClient.Close()
		_ = exec.Command("docker", "rm", "-f", redisName).Run()
	}()

	configPath := filepath.Join(evidenceDir, "relay.yml")
	logPath := filepath.Join(evidenceDir, "relay-server.log")
	result.ConfigPath = "relay.yml"
	result.LogPath = "relay-server.log"
	if err := writeConfig(
		configPath,
		relayKeyPath,
		redisPort,
		backendAddress,
		relayAuthority,
		fmt.Sprintf("127.0.0.1:%d", metricsPort),
	); err != nil {
		return result, err
	}

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := startRelay(
		parent,
		relayBinary,
		configPath,
		caPath,
		logPath,
	)
	if err != nil {
		return result, err
	}
	defer process.close()
	result.ProcessPID = process.cmd.Process.Pid

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return result, errors.New("unable to trust generated CA")
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    roots,
			},
		},
	}
	if err := waitForOK(client, relayActor); err != nil {
		return result, err
	}

	inboxURL := "https://" + relayAuthority + "/inbox"

	first, firstBody, err := signedActivityRequest(
		inboxURL,
		originActor,
		originKey,
		"valid-one",
	)
	if err != nil {
		return result, err
	}
	result.ValidStatus, _, err = send(client, first)
	if err != nil {
		return result, err
	}

	replay, err := cloneSignedRequest(first, firstBody)
	if err != nil {
		return result, err
	}
	result.ReplayStatus, _, err = send(client, replay)
	if err != nil {
		return result, err
	}

	second, secondBody, err := signedActivityRequest(
		inboxURL,
		originActor,
		originKey,
		"valid-two",
	)
	if err != nil {
		return result, err
	}
	tamperedBody := bytes.Replace(
		secondBody,
		[]byte(`"type":"Like"`),
		[]byte(`"type":"Create"`),
		1,
	)
	tampered, err := cloneSignedRequest(second, tamperedBody)
	if err != nil {
		return result, err
	}
	result.TamperedStatus, _, err = send(client, tampered)
	if err != nil {
		return result, err
	}

	validAfterTamper, err := cloneSignedRequest(second, secondBody)
	if err != nil {
		return result, err
	}
	result.ValidAfterTamperStatus, _, err = send(
		client,
		validAfterTamper,
	)
	if err != nil {
		return result, err
	}

	result.SignedActorGETs = signedGETs.Load()
	result.InvalidActorGETs = invalidGETs.Load()

	ctx, cancelRedis := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancelRedis()

	nonceKeys, err := redisClient.Keys(
		ctx,
		"relay:http-signature:nonce:*",
	).Result()
	if err != nil {
		return result, err
	}
	result.NonceMarkerCount = len(nonceKeys)
	result.Metrics, err = redisClient.HGetAll(
		ctx,
		"relay:metrics:operational",
	).Result()
	if err != nil {
		return result, err
	}

	successMetric := result.Metrics["http_signature_verifications_total|rfc9421|success|accepted"]
	replayMetric := result.Metrics["http_signature_verifications_total|rfc9421|failure|replay"]
	digestMetric := result.Metrics["http_signature_verifications_total|rfc9421|failure|digest"]

	if result.ValidStatus != http.StatusAccepted ||
		result.ReplayStatus != http.StatusBadRequest ||
		result.TamperedStatus != http.StatusBadRequest ||
		result.ValidAfterTamperStatus != http.StatusAccepted ||
		result.SignedActorGETs < 2 ||
		result.InvalidActorGETs != 0 ||
		result.NonceMarkerCount != 2 ||
		successMetric != "2" ||
		replayMetric != "1" ||
		digestMetric != "1" {
		result.Classification = "invariant_failure"
		return result, nil
	}

	result.Classification = "rfc9421_inbound_runtime_pass"
	return result, nil
}

func main() {
	result, runErr := run()

	evidenceDir := ""
	for index, argument := range os.Args {
		if argument == "--evidence-dir" && index+1 < len(os.Args) {
			evidenceDir = os.Args[index+1]
		}
	}
	if evidenceDir != "" {
		_ = os.MkdirAll(evidenceDir, 0o700)
		data, _ := json.MarshalIndent(result, "", "  ")
		_ = os.WriteFile(
			filepath.Join(evidenceDir, "report.json"),
			append(data, '\n'),
			0o600,
		)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))

	if runErr != nil || result.Classification !=
		"rfc9421_inbound_runtime_pass" {
		os.Exit(1)
	}
}
