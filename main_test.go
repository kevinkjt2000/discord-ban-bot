package main

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestShouldBan_WrongChannel(t *testing.T) {
	msg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "other-channel",
			Author: &discordgo.User{
				ID:       "user123",
				Username: "testuser",
				Bot:      false,
			},
		},
	}

	if shouldBan("honeypot", msg) {
		t.Error("expected shouldBan to be false for wrong channel")
	}
}

func TestShouldBan_BotUser(t *testing.T) {
	msg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "honeypot",
			Author: &discordgo.User{
				ID:       "bot123",
				Username: "testbot",
				Bot:      true,
			},
		},
	}

	if shouldBan("honeypot", msg) {
		t.Error("expected shouldBan to be false for bot user")
	}
}

func TestShouldBan_ValidUser(t *testing.T) {
	msg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "honeypot",
			Author: &discordgo.User{
				ID:       "user123",
				Username: "testuser",
				Bot:      false,
			},
		},
	}

	if !shouldBan("honeypot", msg) {
		t.Error("expected shouldBan to be true for valid user in honeypot channel")
	}
}

func TestShouldBan_EmptyHoneypot(t *testing.T) {
	msg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "",
			Author: &discordgo.User{
				ID:       "user123",
				Username: "testuser",
				Bot:      false,
			},
		},
	}

	if shouldBan("", msg) {
		t.Error("expected shouldBan to be false when honeypot ID is empty")
	}
}

func TestMessageCache_AddAndPurge(t *testing.T) {
	cache := newMessageCache(5 * time.Second)
	cache.add("user1", "msg1", "ch1")
	cache.add("user1", "msg2", "ch2")
	cache.add("user2", "msg3", "ch3")

	msgs := cache.purge("user1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for user1, got %d", len(msgs))
	}

	// Verify map entry removed
	cache.mu.RLock()
	if _, ok := cache.messages["user1"]; ok {
		t.Error("expected user1 to be removed from cache")
	}
	cache.mu.RUnlock()

	// Verify user2 unaffected
	msgs = cache.purge("user2")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for user2, got %d", len(msgs))
	}
}

func TestMessageCache_PurgeExpired(t *testing.T) {
	cache := newMessageCache(50 * time.Millisecond)
	cache.add("user1", "msg1", "ch1")
	time.Sleep(100 * time.Millisecond)

	msgs := cache.purge("user1")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after expiry, got %d", len(msgs))
	}
}

func TestMessageCache_Cleanup(t *testing.T) {
	cache := newMessageCache(50 * time.Millisecond)
	cache.add("user1", "msg1", "ch1")
	cache.add("user2", "msg2", "ch2")
	time.Sleep(100 * time.Millisecond)

	cache.cleanup()

	cache.mu.RLock()
	if len(cache.messages) != 0 {
		t.Fatalf("expected empty cache after cleanup, got %d entries", len(cache.messages))
	}
	cache.mu.RUnlock()
}

func TestMessageCache_CleanupPartial(t *testing.T) {
	cache := newMessageCache(50 * time.Millisecond)
	cache.add("user1", "msg1", "ch1")
	time.Sleep(60 * time.Millisecond)
	cache.add("user1", "msg2", "ch2")

	cache.cleanup()

	cache.mu.RLock()
	msgs, ok := cache.messages["user1"]
	cache.mu.RUnlock()
	if !ok {
		t.Fatal("expected user1 to still be in cache")
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 valid message after cleanup, got %d", len(msgs))
	}
	if msgs[0].messageID != "msg2" {
		t.Fatalf("expected msg2 to remain, got %s", msgs[0].messageID)
	}
}
