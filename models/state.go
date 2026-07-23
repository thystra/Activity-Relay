package models

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Config : Enum for RelayConfig
type Config int

const (
	// PersonOnly : Limited for Person-Type Actor
	PersonOnly Config = iota
	// ManuallyAccept : Manually Accept Follow-Request
	ManuallyAccept
)

// RelayState : Store Subscribers, Followers And Relay Configurations
type RelayState struct {
	RedisClient *redis.Client `json:"-"`
	notifiable  bool
	mu          *sync.RWMutex

	RelayConfig             relayConfig  `json:"relayConfig,omitempty"`
	LimitedDomains          []string     `json:"limitedDomains,omitempty"`
	BlockedDomains          []string     `json:"blockedDomains,omitempty"`
	Subscribers             []Subscriber `json:"subscriptions,omitempty"`
	Followers               []Follower   `json:"followers,omitempty"`
	SubscribersAndFollowers []Subscriber `json:"-"`
	limitedDomains          map[string]struct{}
	blockedDomains          map[string]struct{}
	subscribersByDomain     map[string]Subscriber
	followersByDomain       map[string]Follower
}

// RelayStateSnapshot is an immutable point-in-time view safe for concurrent readers.
type RelayStateSnapshot struct {
	RelayConfig             relayConfig
	LimitedDomains          []string
	BlockedDomains          []string
	Subscribers             []Subscriber
	Followers               []Follower
	SubscribersAndFollowers []Subscriber
}

// NewState : Create new RelayState instance with redis client
func NewState(redisClient *redis.Client, notifiable bool) RelayState {
	var config RelayState
	config.RedisClient = redisClient
	config.notifiable = notifiable
	config.mu = new(sync.RWMutex)

	if err := config.Load(); err != nil {
		logrus.Error("Unable to load relay state: ", err)
	}
	return config
}

func (config *RelayState) ListenNotify(c chan<- bool) {
	pubsub := config.RedisClient.Subscribe(context.Background(), "relay_refresh")
	_, err := pubsub.Receive(context.Background())
	if err != nil {
		_ = pubsub.Close()
		panic(err)
	}
	ch := pubsub.Channel()

	cNotify := c != nil
	go func() {
		defer pubsub.Close()
		for range ch {
			logrus.Info("RelayState reloaded")
			if err := config.Load(); err != nil {
				logrus.Error("Unable to reload relay state: ", err)
				continue
			}
			if cNotify {
				c <- true
			}
		}
	}()
}

// ScanKeys returns matching Redis keys without blocking the server with KEYS.
func ScanKeys(ctx context.Context, client *redis.Client, pattern string) ([]string, error) {
	keys := make([]string, 0)
	iterator := client.Scan(ctx, 0, pattern, 256).Iterator()
	for iterator.Next(ctx) {
		keys = append(keys, iterator.Val())
	}
	if err := iterator.Err(); err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

func loadHashes(ctx context.Context, client *redis.Client, keys []string, fields ...string) ([][]interface{}, error) {
	pipe := client.Pipeline()
	commands := make([]*redis.SliceCmd, len(keys))
	for i, key := range keys {
		commands[i] = pipe.HMGet(ctx, key, fields...)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	values := make([][]interface{}, len(commands))
	for i, command := range commands {
		values[i] = command.Val()
	}
	return values, nil
}

func stringValue(values []interface{}, index int) string {
	if index >= len(values) || values[index] == nil {
		return ""
	}
	value, _ := values[index].(string)
	return value
}

// Load refreshes relay state from Redis and atomically publishes a complete snapshot.
func (config *RelayState) Load() error {
	ctx := context.Background()
	relayConfiguration, err := loadRelayConfig(ctx, config.RedisClient)
	if err != nil {
		return err
	}
	var limitedDomains []string
	var blockedDomains []string
	var subscribers []Subscriber
	var followers []Follower
	var subscribersAndFollowers []Subscriber

	limitedDomains, err = config.RedisClient.HKeys(ctx, "relay:config:limitedDomain").Result()
	if err != nil {
		return err
	}
	blockedDomains, err = config.RedisClient.HKeys(ctx, "relay:config:blockedDomain").Result()
	if err != nil {
		return err
	}
	sort.Strings(limitedDomains)
	sort.Strings(blockedDomains)

	domains, err := ScanKeys(ctx, config.RedisClient, "relay:subscription:*")
	if err != nil {
		return err
	}
	values, err := loadHashes(ctx, config.RedisClient, domains, "inbox_url", "activity_id", "actor_id")
	if err != nil {
		return err
	}
	for i, domain := range domains {
		domainName := strings.TrimPrefix(domain, "relay:subscription:")
		inboxURL := stringValue(values[i], 0)
		activityID := stringValue(values[i], 1)
		actorID := stringValue(values[i], 2)
		subscribers = append(subscribers, Subscriber{domainName, inboxURL, activityID, actorID})
		subscribersAndFollowers = append(subscribersAndFollowers, Subscriber{domainName, inboxURL, activityID, actorID})
	}

	domains, err = ScanKeys(ctx, config.RedisClient, "relay:follower:*")
	if err != nil {
		return err
	}
	values, err = loadHashes(ctx, config.RedisClient, domains, "inbox_url", "activity_id", "actor_id", "mutually_follow")
	if err != nil {
		return err
	}
	for i, domain := range domains {
		domainName := strings.TrimPrefix(domain, "relay:follower:")
		inboxURL := stringValue(values[i], 0)
		activityID := stringValue(values[i], 1)
		actorID := stringValue(values[i], 2)
		mutuallyFollow := stringValue(values[i], 3)
		followers = append(followers, Follower{domainName, inboxURL, activityID, actorID, mutuallyFollow == "1"})
		subscribersAndFollowers = append(subscribersAndFollowers, Subscriber{domainName, inboxURL, activityID, actorID})
	}

	limitedSet := make(map[string]struct{}, len(limitedDomains))
	for _, domain := range limitedDomains {
		limitedSet[domain] = struct{}{}
	}
	blockedSet := make(map[string]struct{}, len(blockedDomains))
	for _, domain := range blockedDomains {
		blockedSet[domain] = struct{}{}
	}
	subscriberSet := make(map[string]Subscriber, len(subscribers))
	for _, subscriber := range subscribers {
		subscriberSet[subscriber.Domain] = subscriber
	}
	followerSet := make(map[string]Follower, len(followers))
	for _, follower := range followers {
		followerSet[follower.Domain] = follower
	}

	config.mu.Lock()
	defer config.mu.Unlock()
	config.RelayConfig = relayConfiguration
	config.LimitedDomains = limitedDomains
	config.BlockedDomains = blockedDomains
	config.Subscribers = subscribers
	config.Followers = followers
	config.SubscribersAndFollowers = subscribersAndFollowers
	config.limitedDomains = limitedSet
	config.blockedDomains = blockedSet
	config.subscribersByDomain = subscriberSet
	config.followersByDomain = followerSet
	return nil
}

// Snapshot returns copies of the state slices so callers can iterate without races.
func (config *RelayState) Snapshot() RelayStateSnapshot {
	config.mu.RLock()
	defer config.mu.RUnlock()
	return RelayStateSnapshot{
		RelayConfig:             config.RelayConfig,
		LimitedDomains:          append([]string(nil), config.LimitedDomains...),
		BlockedDomains:          append([]string(nil), config.BlockedDomains...),
		Subscribers:             append([]Subscriber(nil), config.Subscribers...),
		Followers:               append([]Follower(nil), config.Followers...),
		SubscribersAndFollowers: append([]Subscriber(nil), config.SubscribersAndFollowers...),
	}
}

func (config *RelayState) IsLimited(domain string) bool {
	config.mu.RLock()
	defer config.mu.RUnlock()
	_, ok := config.limitedDomains[domain]
	return ok
}

func (config *RelayState) IsBlocked(domain string) bool {
	config.mu.RLock()
	defer config.mu.RUnlock()
	_, ok := config.blockedDomains[domain]
	return ok
}

func (config *RelayState) IsSubscriber(domain string) bool {
	config.mu.RLock()
	defer config.mu.RUnlock()
	_, ok := config.subscribersByDomain[domain]
	return ok
}

func (config *RelayState) IsFollower(domain string) bool {
	config.mu.RLock()
	defer config.mu.RUnlock()
	_, ok := config.followersByDomain[domain]
	return ok
}

func (config *RelayState) IsSubscriberOrFollower(domain string) bool {
	config.mu.RLock()
	defer config.mu.RUnlock()
	_, subscriber := config.subscribersByDomain[domain]
	_, follower := config.followersByDomain[domain]
	return subscriber || follower
}

func (config *RelayState) PersonOnly() bool {
	config.mu.RLock()
	defer config.mu.RUnlock()
	return config.RelayConfig.PersonOnly
}

func (config *RelayState) ManualApprovalRequired() bool {
	config.mu.RLock()
	defer config.mu.RUnlock()
	return config.RelayConfig.ManuallyAccept
}

// SetConfig : Set relay configuration
func (config *RelayState) SetConfig(key Config, value bool) {
	strValue := 0
	if value {
		strValue = 1
	}
	switch key {
	case PersonOnly:
		config.RedisClient.HSet(context.TODO(), "relay:config", "block_service", strValue).Result()
	case ManuallyAccept:
		config.RedisClient.HSet(context.TODO(), "relay:config", "manually_accept", strValue).Result()
	}

	config.refresh()
}

// AddSubscriber : Add new instance for subscriber list
func (config *RelayState) AddSubscriber(domain Subscriber) {
	config.RedisClient.HMSet(context.TODO(), "relay:subscription:"+domain.Domain, map[string]interface{}{
		"inbox_url":   domain.InboxURL,
		"activity_id": domain.ActivityID,
		"actor_id":    domain.ActorID,
	})

	config.refresh()
}

// DelSubscriber : Delete instance from subscriber list
func (config *RelayState) DelSubscriber(domain string) {
	config.RedisClient.Del(context.TODO(), "relay:subscription:"+domain).Result()
	config.RedisClient.Del(context.TODO(), "relay:pending:"+domain).Result()

	config.refresh()
}

// SelectSubscriber : Select instance from subscriber list
func (config *RelayState) SelectSubscriber(domain string) *Subscriber {
	config.mu.RLock()
	defer config.mu.RUnlock()
	if subscriber, ok := config.subscribersByDomain[domain]; ok {
		return &subscriber
	}
	return nil
}

// AddFollower : Add new instance for follower list
func (config *RelayState) AddFollower(domain Follower) {
	config.RedisClient.HMSet(context.TODO(), "relay:follower:"+domain.Domain, map[string]interface{}{
		"inbox_url":       domain.InboxURL,
		"activity_id":     domain.ActivityID,
		"actor_id":        domain.ActorID,
		"mutually_follow": domain.MutuallyFollow,
	})

	config.refresh()
}

// UpdateFollowerStatus : Update MutuallyFollow Status
func (config *RelayState) UpdateFollowerStatus(domain string, mutuallyFollow bool) {
	if mutuallyFollow {
		config.RedisClient.HSet(context.TODO(), "relay:follower:"+domain, "mutually_follow", "1")
	} else {
		config.RedisClient.HSet(context.TODO(), "relay:follower:"+domain, "mutually_follow", "0")
	}

	config.refresh()
}

// DelFollower : Delete instance from follower list
func (config *RelayState) DelFollower(domain string) {
	config.RedisClient.Del(context.TODO(), "relay:follower:"+domain).Result()
	config.RedisClient.Del(context.TODO(), "relay:pending:"+domain).Result()

	config.refresh()
}

// SelectFollower : Select instance from follower list
func (config *RelayState) SelectFollower(domain string) *Follower {
	config.mu.RLock()
	defer config.mu.RUnlock()
	if follower, ok := config.followersByDomain[domain]; ok {
		return &follower
	}
	return nil
}

// SetBlockedDomain : Set/Unset instance for blocked domain
func (config *RelayState) SetBlockedDomain(domain string, value bool) {
	if value {
		config.RedisClient.HSet(context.TODO(), "relay:config:blockedDomain", domain, "1").Result()
	} else {
		config.RedisClient.HDel(context.TODO(), "relay:config:blockedDomain", domain).Result()
	}

	config.refresh()
}

// SetLimitedDomain : Set/Unset instance for limited domain
func (config *RelayState) SetLimitedDomain(domain string, value bool) {
	if value {
		config.RedisClient.HSet(context.TODO(), "relay:config:limitedDomain", domain, "1").Result()
	} else {
		config.RedisClient.HDel(context.TODO(), "relay:config:limitedDomain", domain).Result()
	}

	config.refresh()
}

func (config *RelayState) refresh() {
	if config.notifiable {
		config.RedisClient.Publish(context.TODO(), "relay_refresh", nil)
	} else {
		config.Load()
	}
}

// Subscriber : Manage for Mastodon Traditional Style Relay Subscriber
type Subscriber struct {
	Domain     string `json:"domain,omitempty"`
	InboxURL   string `json:"inbox_url,omitempty"`
	ActivityID string `json:"activity_id,omitempty"`
	ActorID    string `json:"actor_id,omitempty"`
}

// Follower : Manage for LitePub Style Relay Follower
type Follower struct {
	Domain         string `json:"domain,omitempty"`
	InboxURL       string `json:"inbox_url,omitempty"`
	ActivityID     string `json:"activity_id,omitempty"`
	ActorID        string `json:"actor_id,omitempty"`
	MutuallyFollow bool   `json:"mutually_follow,omitempty"`
}

type relayConfig struct {
	PersonOnly     bool `json:"blockService,omitempty"`
	ManuallyAccept bool `json:"manuallyAccept,omitempty"`
}

func loadRelayConfig(ctx context.Context, redisClient *redis.Client) (relayConfig, error) {
	values, err := redisClient.HMGet(ctx, "relay:config", "block_service", "manually_accept").Result()
	if err != nil {
		return relayConfig{}, err
	}
	return relayConfig{
		PersonOnly:     stringValue(values, 0) == "1",
		ManuallyAccept: stringValue(values, 1) == "1",
	}, nil
}
