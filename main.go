package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

type cachedMessage struct {
	messageID string
	channelID string
	timestamp time.Time
}

type messageCache struct {
	mu       sync.RWMutex
	messages map[string][]cachedMessage
	ttl      time.Duration
}

func newMessageCache(ttl time.Duration) *messageCache {
	return &messageCache{
		messages: make(map[string][]cachedMessage),
		ttl:      ttl,
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
	for userID, msgs := range c.messages {
		var valid []cachedMessage
		for _, m := range msgs {
			if now.Sub(m.timestamp) <= c.ttl {
				valid = append(valid, m)
			}
		}
		if len(valid) == 0 {
			delete(c.messages, userID)
		} else {
			c.messages[userID] = valid
		}
	}
}

func shouldBan(honeypotID string, m *discordgo.MessageCreate) bool {
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

func handleMessage(s *discordgo.Session, honeypotID string, cache *messageCache, m *discordgo.MessageCreate) {
	if m.Author != nil && !m.Author.Bot {
		cache.add(m.Author.ID, m.ID, m.ChannelID)
	}

	if !shouldBan(honeypotID, m) {
		return
	}

	log.Printf("honeypot triggered: user=%s (%s) message=%s channel=%s",
		m.Author.Username, m.Author.ID, m.ID, m.ChannelID)

	err := s.GuildBanCreateWithReason(m.GuildID, m.Author.ID, "Posted in honeypot channel", 0)
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
	if len(msgs) == 0 {
		return
	}

	log.Printf("purging %d cached message(s) from user %s", len(msgs), m.Author.ID)
	for _, msg := range msgs {
		if msg.channelID == honeypotID {
			continue
		}
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

	cache := newMessageCache(ttl)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cache.cleanup()
		}
	}()

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("failed to create discord session: %v", err)
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		handleMessage(s, honeypotID, cache, m)
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
