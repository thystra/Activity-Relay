package deliver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/thystra/Activity-Relay/models"
)

func TestMain(m *testing.M) {
	var err error

	testConfigPath := "../misc/test/config.yml"
	file, _ := os.Open(testConfigPath)
	defer file.Close()

	viper.SetConfigType("yaml")
	viper.ReadConfig(file)
	viper.Set("ACTOR_PEM", "../misc/test/testKey.pem")
	viper.BindEnv("REDIS_URL")

	GlobalConfig, err = models.NewRelayConfig()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	err = initialize(GlobalConfig)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	RedisClient.FlushAll(context.TODO()).Result()
	code := m.Run()
	os.Exit(code)
}

func TestRelayActivity(t *testing.T) {
	RedisClient.FlushAll(context.Background()).Result()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if string(data) != "ExampleData" || r.Header.Get("Content-Type") != "application/activity+json" {
			w.WriteHeader(500)
			w.Write(nil)
		} else {
			w.WriteHeader(202)
			w.Write(nil)
		}
	}))
	defer s.Close()

	activityID := uuid.New()
	remainCount := 1

	pushActivityScript := "redis.call('HSET',KEYS[1], 'body', ARGV[1], 'remain_count', ARGV[2]); redis.call('EXPIRE', KEYS[1], ARGV[3]);"
	RedisClient.Eval(context.TODO(), pushActivityScript, []string{"relay:activity:" + activityID.String()}, "ExampleData", remainCount, 10).Result()

	err := relayActivityV2(context.Background(), s.URL, activityID.String())
	if err != nil {
		t.Fatal(err)
	}
	domain, err := models.ReceiverDomainFromInboxURL(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	health, err := models.LoadReceiverDeliveryHealth(context.Background(), RedisClient, []string{domain})
	if err != nil {
		t.Fatal(err)
	}
	got := health[domain]
	if got.TotalSuccesses != 1 || got.TotalFailures != 0 || got.ConsecutiveFailures != 0 || got.LastSuccessAt == "" {
		t.Fatalf("unexpected successful delivery health: %+v", got)
	}
}

func TestRelayActivityNoHost(t *testing.T) {
	RedisClient.FlushAll(context.Background()).Result()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	}))
	defer s.Close()

	activityID := uuid.New()
	remainCount := 1

	pushActivityScript := "redis.call('HSET',KEYS[1], 'body', ARGV[1], 'remain_count', ARGV[2]); redis.call('EXPIRE', KEYS[1], ARGV[3]);"
	RedisClient.Eval(context.TODO(), pushActivityScript, []string{"relay:activity:" + activityID.String()}, "ExampleData", remainCount, 10).Result()

	err := relayActivityV2(context.Background(), "http://nohost.example.jp", activityID.String())
	if err == nil {
		t.Fatal("Expected error to be reported for nohost, but got nil")
	}
	domain, _ := url.Parse("http://nohost.example.jp")
	data, _ := RedisClient.HGet(context.TODO(), "relay:statistics:"+domain.Host, "last_error").Result()
	if data == "" {
		t.Fatalf("Expected last_error to be saved for domain %s, but got empty string", domain.Host)
	}
	health, healthErr := models.LoadReceiverDeliveryHealth(context.Background(), RedisClient, []string{domain.Host})
	if healthErr != nil {
		t.Fatal(healthErr)
	}
	got := health[domain.Host]
	if got.TotalFailures != 1 || got.ConsecutiveFailures != 1 || got.TotalSuccesses != 0 || got.LastFailureAt == "" {
		t.Fatalf("unexpected failed delivery health: %+v", got)
	}
}

func TestRelayActivityResp500(t *testing.T) {
	RedisClient.FlushAll(context.Background()).Result()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write(nil)
	}))
	defer s.Close()

	activityID := uuid.New()
	remainCount := 1

	pushActivityScript := "redis.call('HSET',KEYS[1], 'body', ARGV[1], 'remain_count', ARGV[2]); redis.call('EXPIRE', KEYS[1], ARGV[3]);"
	RedisClient.Eval(context.TODO(), pushActivityScript, []string{"relay:activity:" + activityID.String()}, "ExampleData", remainCount, 10).Result()

	err := relayActivityV2(context.Background(), s.URL, activityID.String())
	if err == nil {
		t.Fatal("Expected error to be reported for 500 response, but got nil")
	}
	domain, _ := url.Parse(s.URL)
	data, _ := RedisClient.HGet(context.TODO(), "relay:statistics:"+domain.Host, "last_error").Result()
	if data == "" {
		t.Fatalf("Expected last_error to be saved for domain %s, but got empty string", domain.Host)
	}
	health, healthErr := models.LoadReceiverDeliveryHealth(context.Background(), RedisClient, []string{domain.Host})
	if healthErr != nil {
		t.Fatal(healthErr)
	}
	got := health[domain.Host]
	if got.TotalFailures != 1 || got.ConsecutiveFailures != 1 || got.TotalSuccesses != 0 || got.LastFailureAt == "" {
		t.Fatalf("unexpected 500 delivery health: %+v", got)
	}
}

func TestRegisterActivity(t *testing.T) {
	RedisClient.FlushAll(context.Background()).Result()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if string(data) != "data" || r.Header.Get("Content-Type") != "application/activity+json" {
			w.WriteHeader(500)
			w.Write(nil)
		} else {
			w.WriteHeader(202)
			w.Write(nil)
		}
	}))
	defer s.Close()

	err := registerActivity(s.URL, "data")
	if err != nil {
		t.Fatalf("Expected registerActivity to succeed, but got error: %v", err)
	}
	domain, err := models.ReceiverDomainFromInboxURL(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	exists, err := RedisClient.Exists(context.Background(), "relay:receiver-health:"+domain).Result()
	if err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatal("registration delivery must not create receiver fan-out health")
	}
}

func TestRegisterActivityNoHost(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	}))
	defer s.Close()

	err := registerActivity("http://nohost.example.jp", "data")
	if err == nil {
		t.Fatal("Expected error to be reported for nohost, but got nil")
	}
}

func TestRegisterActivityResp500(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write(nil)
	}))
	defer s.Close()

	err := registerActivity(s.URL, "data")
	if err == nil {
		t.Fatal("Expected error to be reported for 500 response, but got nil")
	}
}
