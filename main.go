package main

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

type discordSession interface {
	GuildBanCreateWithReason(guildID, userID, reason string, days int, options ...discordgo.RequestOption) error
	ChannelMessageDelete(channelID, messageID string, options ...discordgo.RequestOption) error
}

type cachedMessage struct {
	messageID string
	channelID string
	timestamp time.Time
}

type messageCache struct {
	mu         sync.RWMutex
	messages   map[string][]cachedMessage
	ttl        time.Duration
	maxPerUser int
}

func newMessageCache(ttl time.Duration, maxPerUser int) *messageCache {
	if maxPerUser <= 0 {
		maxPerUser = 100
	}
	return &messageCache{
		messages:   make(map[string][]cachedMessage),
		ttl:        ttl,
		maxPerUser: maxPerUser,
	}
}

func (c *messageCache) add(userID, messageID, channelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages[userID] = append(c.messages[userID], cachedMessage{
		messageID: messageID,
		channelID: channelID,
		timestamp: time.Now(),
	})
	if len(c.messages[userID]) > c.maxPerUser {
		c.messages[userID] = c.messages[userID][len(c.messages[userID])-c.maxPerUser:]
	}
}

func (c *messageCache) purge(userID string) []cachedMessage {
	c.mu.Lock()
	defer c.mu.Unlock()

	msgs, ok := c.messages[userID]
	if !ok {
		return nil
	}
	delete(c.messages, userID)

	now := time.Now()
	var valid []cachedMessage
	for _, m := range msgs {
		if now.Sub(m.timestamp) <= c.ttl {
			valid = append(valid, m)
		}
	}
	return valid
}

func (c *messageCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	prunedUsers := 0
	for userID, msgs := range c.messages {
		var valid []cachedMessage
		for _, m := range msgs {
			if now.Sub(m.timestamp) <= c.ttl {
				valid = append(valid, m)
			}
		}
		if len(valid) == 0 {
			delete(c.messages, userID)
			prunedUsers++
		} else {
			c.messages[userID] = valid
		}
	}
	if prunedUsers > 0 {
		log.Printf("cache cleanup: pruned %d user(s), %d user(s) remain", prunedUsers, len(c.messages))
	}
}

type spamCache struct {
	mu      sync.RWMutex
	entries map[string]map[string][]cachedMessage // hash -> userID -> entries
	ttl     time.Duration
}

func newSpamCache(ttl time.Duration) *spamCache {
	return &spamCache{
		entries: make(map[string]map[string][]cachedMessage),
		ttl:     ttl,
	}
}

func hashMessage(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func (sc *spamCache) add(userID, messageID, channelID, content string) {
	hash := hashMessage(content)

	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.entries[hash] == nil {
		sc.entries[hash] = make(map[string][]cachedMessage)
	}
	sc.entries[hash][userID] = append(sc.entries[hash][userID], cachedMessage{
		channelID: channelID,
		messageID: messageID,
		timestamp: time.Now(),
	})
}

func (sc *spamCache) check(userID, content string) ([]cachedMessage, bool) {
	hash := hashMessage(content)

	sc.mu.RLock()
	defer sc.mu.RUnlock()

	userEntries, ok := sc.entries[hash][userID]
	if !ok {
		return nil, false
	}

	now := time.Now()
	var valid []cachedMessage
	seenChannels := make(map[string]struct{})
	for _, e := range userEntries {
		if now.Sub(e.timestamp) <= sc.ttl {
			valid = append(valid, e)
			seenChannels[e.channelID] = struct{}{}
		}
	}

	if len(seenChannels) >= 3 {
		return valid, true
	}
	return nil, false
}

func (sc *spamCache) cleanup() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()
	for hash, users := range sc.entries {
		for userID, entries := range users {
			var valid []cachedMessage
			for _, e := range entries {
				if now.Sub(e.timestamp) <= sc.ttl {
					valid = append(valid, e)
				}
			}
			if len(valid) == 0 {
				delete(users, userID)
			} else {
				users[userID] = valid
			}
		}
		if len(users) == 0 {
			delete(sc.entries, hash)
		}
	}
}

func shouldBanHoneypot(honeypotID string, m *discordgo.MessageCreate) bool {
	if honeypotID == "" {
		return false
	}
	if m.ChannelID != honeypotID {
		return false
	}
	if m.Author == nil || m.Author.Bot {
		return false
	}
	return true
}

func shouldBanSpam(m *discordgo.MessageCreate) bool {
	if m.Author == nil || m.Author.Bot {
		return false
	}
	return true
}

func banAndPurge(s discordSession, cache *messageCache, m *discordgo.MessageCreate, reason string, extraMsgs []cachedMessage) {
	log.Printf("banning user=%s (%s) reason=%s message=%s channel=%s",
		m.Author.Username, m.Author.ID, reason, m.ID, m.ChannelID)

	err := s.GuildBanCreateWithReason(m.GuildID, m.Author.ID, reason, 0)
	if err != nil {
		if restErr, ok := err.(*discordgo.RESTError); ok && restErr.Message != nil {
			log.Printf("ban failed for user %s: discord API error %d: %s",
				m.Author.ID, restErr.Response.StatusCode, restErr.Message.Message)
		} else {
			log.Printf("ban failed for user %s: %v", m.Author.ID, err)
		}
		return
	}

	log.Printf("banned user %s (%s)", m.Author.Username, m.Author.ID)

	msgs := cache.purge(m.Author.ID)
	if len(msgs) == 0 && len(extraMsgs) == 0 {
		return
	}

	allMsgs := append(msgs, extraMsgs...)
	// deduplicate by messageID
	seen := make(map[string]struct{}, len(allMsgs))
	var uniq []cachedMessage
	for _, msg := range allMsgs {
		if _, ok := seen[msg.messageID]; ok {
			continue
		}
		seen[msg.messageID] = struct{}{}
		uniq = append(uniq, msg)
	}

	log.Printf("purging %d message(s) from user %s", len(uniq), m.Author.ID)
	for _, msg := range uniq {
		err := s.ChannelMessageDelete(msg.channelID, msg.messageID)
		if err != nil {
			if restErr, ok := err.(*discordgo.RESTError); ok && restErr.Message != nil {
				log.Printf("delete failed for message %s in channel %s: discord API error %d: %s",
					msg.messageID, msg.channelID, restErr.Response.StatusCode, restErr.Message.Message)
			} else {
				log.Printf("delete failed for message %s in channel %s: %v",
					msg.messageID, msg.channelID, err)
			}
			continue
		}
		log.Printf("deleted message %s from channel %s", msg.messageID, msg.channelID)
	}
}

func handleMessage(s discordSession, honeypotID string, cache *messageCache, spamCache *spamCache, m *discordgo.MessageCreate) {
	if m.Author != nil && !m.Author.Bot {
		cache.add(m.Author.ID, m.ID, m.ChannelID)
		spamCache.add(m.Author.ID, m.ID, m.ChannelID, m.Content)
	}

	// Check spam first
	if shouldBanSpam(m) {
		spamMsgs, isSpam := spamCache.check(m.Author.ID, m.Content)
		if isSpam {
			banAndPurge(s, cache, m, "Spam: same message in 3+ channels", spamMsgs)
			return
		}
	}

	// Check honeypot
	if shouldBanHoneypot(honeypotID, m) {
		banAndPurge(s, cache, m, "Posted in honeypot channel", nil)
		return
	}
}

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN environment variable is required")
	}

	honeypotID := os.Getenv("HONEYPOT_CHANNEL_ID")
	if honeypotID == "" {
		log.Fatal("HONEYPOT_CHANNEL_ID environment variable is required")
	}

	ttl := 60 * time.Second
	if v := os.Getenv("CACHE_TTL_SECONDS"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil {
			ttl = d
		} else {
			log.Printf("invalid CACHE_TTL_SECONDS, using default 60s: %v", err)
		}
	}

	maxPerUser := 100
	if v := os.Getenv("CACHE_MAX_PER_USER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxPerUser = n
		} else {
			log.Printf("invalid CACHE_MAX_PER_USER, using default 100: %v", err)
		}
	}

	cache := newMessageCache(ttl, maxPerUser)
	spamCache := newSpamCache(ttl)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cache.cleanup()
			spamCache.cleanup()
		}
	}()

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("failed to create discord session: %v", err)
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		handleMessage(s, honeypotID, cache, spamCache, m)
	})

	dg.Identify.Intents = discordgo.IntentsGuildMessages

	err = dg.Open()
	if err != nil {
		if strings.Contains(err.Error(), "4014") {
			log.Fatalf("gateway connection failed: privileged intent not enabled (4014). enable GUILD_MESSAGES intent in Discord Developer Portal")
		}
		if strings.Contains(err.Error(), "4011") {
			log.Fatalf("gateway connection failed: bot is in too many guilds without sharding (4011)")
		}
		if strings.Contains(err.Error(), "4013") {
			log.Fatalf("gateway connection failed: invalid intents (4013)")
		}
		if strings.Contains(err.Error(), "4001") {
			log.Fatalf("gateway connection failed: unknown opcode (4001)")
		}
		if strings.Contains(err.Error(), "4002") {
			log.Fatalf("gateway connection failed: decode error (4002)")
		}
		if strings.Contains(err.Error(), "4004") {
			log.Fatalf("gateway connection failed: authentication failed (4004). check your DISCORD_TOKEN")
		}
		if strings.Contains(err.Error(), "4005") {
			log.Fatalf("gateway connection failed: already authenticated (4005)")
		}
		if strings.Contains(err.Error(), "4009") {
			log.Fatalf("gateway connection failed: session timed out (4009)")
		}
		log.Fatalf("failed to open gateway connection: %v", err)
	}
	defer dg.Close()

	log.Printf("ban-bot connected. monitoring honeypot channel %s (cache_ttl=%s)", honeypotID, ttl)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Println("shutting down")
}
