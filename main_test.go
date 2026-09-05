package main

import (
	"errors"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type mockSession struct {
	bannedUserID string
	deletedMsgs  []struct{ channelID, messageID string }
	banErr       error
	deleteErrs   map[string]error
}

func (m *mockSession) GuildBanCreateWithReason(guildID, userID, reason string, days int, options ...discordgo.RequestOption) error {
	m.bannedUserID = userID
	return m.banErr
}

func (m *mockSession) ChannelMessageDelete(channelID, messageID string, options ...discordgo.RequestOption) error {
	m.deletedMsgs = append(m.deletedMsgs, struct{ channelID, messageID string }{channelID, messageID})
	if m.deleteErrs != nil {
		if err, ok := m.deleteErrs[messageID]; ok {
			return err
		}
	}
	return nil
}

func TestHandleMessage_DeletesHoneypotMessage(t *testing.T) {
	cache := newMessageCache(5*time.Second, 100)
	spamCache := newSpamCache(5 * time.Second)
	honeypotID := "honeypot-channel"
	userID := "user123"
	msgHoneypot1 := "msg-honeypot-1"
	msgHoneypot2 := "msg-honeypot-2"
	msgOther := "msg-other"
	otherChannel := "other-channel"
	guildID := "guild123"

	// Seed cache with two messages: one older honeypot message and one other channel message
	cache.add(userID, msgHoneypot1, honeypotID)
	cache.add(userID, msgOther, otherChannel)

	mock := &mockSession{}

	// Trigger with a NEW honeypot message (different ID from the seeded one)
	trigger := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        msgHoneypot2,
			ChannelID: honeypotID,
			GuildID:   guildID,
			Author: &discordgo.User{
				ID:       userID,
				Username: "testuser",
				Bot:      false,
			},
		},
	}

	handleMessage(mock, honeypotID, cache, spamCache, trigger)

	if mock.bannedUserID != userID {
		t.Fatalf("expected user %s to be banned, got %s", userID, mock.bannedUserID)
	}

	// Should have 3 deleted messages: the 2 seeded + the trigger
	if len(mock.deletedMsgs) != 3 {
		t.Fatalf("expected 3 deleted messages, got %d: %+v", len(mock.deletedMsgs), mock.deletedMsgs)
	}

	foundHoneypot1 := false
	foundHoneypot2 := false
	foundOther := false
	for _, dm := range mock.deletedMsgs {
		if dm.messageID == msgHoneypot1 && dm.channelID == honeypotID {
			foundHoneypot1 = true
		}
		if dm.messageID == msgHoneypot2 && dm.channelID == honeypotID {
			foundHoneypot2 = true
		}
		if dm.messageID == msgOther && dm.channelID == otherChannel {
			foundOther = true
		}
	}
	if !foundHoneypot1 {
		t.Errorf("expected seeded honeypot message %s to be deleted", msgHoneypot1)
	}
	if !foundHoneypot2 {
		t.Errorf("expected trigger honeypot message %s to be deleted", msgHoneypot2)
	}
	if !foundOther {
		t.Errorf("expected other message %s to be deleted", msgOther)
	}
}

func TestShouldBanHoneypot_WrongChannel(t *testing.T) {
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

	if shouldBanHoneypot("honeypot", msg) {
		t.Error("expected shouldBanHoneypot to be false for wrong channel")
	}
}

func TestShouldBanHoneypot_BotUser(t *testing.T) {
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

	if shouldBanHoneypot("honeypot", msg) {
		t.Error("expected shouldBanHoneypot to be false for bot user")
	}
}

func TestShouldBanHoneypot_ValidUser(t *testing.T) {
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

	if !shouldBanHoneypot("honeypot", msg) {
		t.Error("expected shouldBanHoneypot to be true for valid user in honeypot channel")
	}
}

func TestShouldBanHoneypot_EmptyHoneypot(t *testing.T) {
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

	if shouldBanHoneypot("", msg) {
		t.Error("expected shouldBanHoneypot to be false when honeypot ID is empty")
	}
}

func TestMessageCache_AddAndPurge(t *testing.T) {
	cache := newMessageCache(5*time.Second, 100)
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
	cache := newMessageCache(50*time.Millisecond, 100)
	cache.add("user1", "msg1", "ch1")
	time.Sleep(100 * time.Millisecond)

	msgs := cache.purge("user1")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after expiry, got %d", len(msgs))
	}
}

func TestMessageCache_Cleanup(t *testing.T) {
	cache := newMessageCache(50*time.Millisecond, 100)
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
	cache := newMessageCache(50*time.Millisecond, 100)
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

func TestMessageCache_MaxPerUser(t *testing.T) {
	cache := newMessageCache(5*time.Second, 3)
	cache.add("user1", "msg1", "ch1")
	cache.add("user1", "msg2", "ch2")
	cache.add("user1", "msg3", "ch3")
	cache.add("user1", "msg4", "ch4")
	cache.add("user1", "msg5", "ch5")

	cache.mu.RLock()
	msgs := cache.messages["user1"]
	cache.mu.RUnlock()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after cap, got %d", len(msgs))
	}
	if msgs[0].messageID != "msg3" {
		t.Fatalf("expected oldest kept to be msg3, got %s", msgs[0].messageID)
	}
	if msgs[2].messageID != "msg5" {
		t.Fatalf("expected newest to be msg5, got %s", msgs[2].messageID)
	}
}

func TestSpamCache_Check_NotSpam(t *testing.T) {
	sc := newSpamCache(5 * time.Second)
	sc.add("user1", "msg1", "ch1", "hello world")
	sc.add("user1", "msg2", "ch2", "hello world")

	msgs, isSpam := sc.check("user1", "hello world")
	if isSpam {
		t.Fatal("expected not spam with only 2 channels")
	}
	if msgs != nil {
		t.Fatalf("expected nil msgs, got %d", len(msgs))
	}
}

func TestSpamCache_Check_Spam(t *testing.T) {
	sc := newSpamCache(5 * time.Second)
	sc.add("user1", "msg1", "ch1", "spam content")
	sc.add("user1", "msg2", "ch2", "spam content")

	// The third message in a third channel should trigger
	sc.add("user1", "msg3", "ch3", "spam content")

	msgs, isSpam := sc.check("user1", "spam content")
	if !isSpam {
		t.Fatal("expected spam with 3 channels")
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 spam entries, got %d", len(msgs))
	}
}

func TestSpamCache_Check_DifferentUsers(t *testing.T) {
	sc := newSpamCache(5 * time.Second)
	sc.add("user1", "msg1", "ch1", "same text")
	sc.add("user2", "msg2", "ch2", "same text")
	sc.add("user2", "msg3", "ch3", "same text")

	msgs, isSpam := sc.check("user1", "same text")
	if isSpam {
		t.Fatal("expected user1 not spam")
	}
	if msgs != nil {
		t.Fatalf("expected nil msgs for user1, got %d", len(msgs))
	}
}

func TestSpamCache_Cleanup(t *testing.T) {
	sc := newSpamCache(50 * time.Millisecond)
	sc.add("user1", "msg1", "ch1", "test")
	time.Sleep(100 * time.Millisecond)

	sc.cleanup()

	sc.mu.RLock()
	if len(sc.entries) != 0 {
		t.Fatalf("expected empty spam cache after cleanup, got %d entries", len(sc.entries))
	}
	sc.mu.RUnlock()
}

func TestSpamCache_CleanupPartial(t *testing.T) {
	sc := newSpamCache(50 * time.Millisecond)
	sc.add("user1", "msg1", "ch1", "test")
	time.Sleep(60 * time.Millisecond)
	sc.add("user1", "msg2", "ch2", "test")

	sc.cleanup()

	sc.mu.RLock()
	users, ok := sc.entries[hashMessage("test")]
	sc.mu.RUnlock()
	if !ok {
		t.Fatal("expected hash to still be in cache")
	}
	entries, ok := users["user1"]
	if !ok {
		t.Fatal("expected user1 to still be in cache")
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 valid entry after cleanup, got %d", len(entries))
	}
	if entries[0].messageID != "msg2" {
		t.Fatalf("expected msg2 to remain, got %s", entries[0].messageID)
	}
}

func TestHandleMessage_SpamBan(t *testing.T) {
	cache := newMessageCache(5*time.Second, 100)
	spamCache := newSpamCache(5 * time.Second)
	userID := "user123"
	guildID := "guild123"
	content := "duplicate spam"

	// Seed 2 messages in 2 channels
	spamCache.add(userID, "msg1", "ch1", content)
	spamCache.add(userID, "msg2", "ch2", content)

	mock := &mockSession{}

	// Third message in third channel triggers ban
	trigger := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg3",
			ChannelID: "ch3",
			GuildID:   guildID,
			Content:   content,
			Author: &discordgo.User{
				ID:       userID,
				Username: "testuser",
				Bot:      false,
			},
		},
	}

	handleMessage(mock, "honeypot", cache, spamCache, trigger)

	if mock.bannedUserID != userID {
		t.Fatalf("expected user %s to be banned for spam, got %s", userID, mock.bannedUserID)
	}

	// Should delete the 3 spam messages
	if len(mock.deletedMsgs) != 3 {
		t.Fatalf("expected 3 deleted messages, got %d: %+v", len(mock.deletedMsgs), mock.deletedMsgs)
	}
}

func TestHandleMessage_BotIgnored(t *testing.T) {
	cache := newMessageCache(5*time.Second, 100)
	spamCache := newSpamCache(5 * time.Second)
	botID := "bot123"
	content := "bot spam"

	spamCache.add(botID, "msg1", "ch1", content)
	spamCache.add(botID, "msg2", "ch2", content)
	spamCache.add(botID, "msg3", "ch3", content)

	mock := &mockSession{}

	trigger := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg4",
			ChannelID: "ch4",
			Content:   content,
			Author: &discordgo.User{
				ID:       botID,
				Username: "testbot",
				Bot:      true,
			},
		},
	}

	handleMessage(mock, "honeypot", cache, spamCache, trigger)

	if mock.bannedUserID != "" {
		t.Fatalf("expected bot not banned, got %s", mock.bannedUserID)
	}
	if len(mock.deletedMsgs) != 0 {
		t.Fatalf("expected 0 deleted messages for bot, got %d", len(mock.deletedMsgs))
	}
}

func TestHandleMessage_BanFailure(t *testing.T) {
	cache := newMessageCache(5*time.Second, 100)
	spamCache := newSpamCache(5 * time.Second)
	userID := "user123"
	content := "fail test"

	spamCache.add(userID, "msg1", "ch1", content)
	spamCache.add(userID, "msg2", "ch2", content)

	mock := &mockSession{banErr: errors.New("api failure")}

	trigger := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg3",
			ChannelID: "ch3",
			Content:   content,
			Author: &discordgo.User{
				ID:       userID,
				Username: "testuser",
				Bot:      false,
			},
		},
	}

	handleMessage(mock, "honeypot", cache, spamCache, trigger)

	if mock.bannedUserID != userID {
		t.Fatalf("expected ban attempt for user %s", userID)
	}
	if len(mock.deletedMsgs) != 0 {
		t.Fatalf("expected 0 deleted messages when ban fails, got %d", len(mock.deletedMsgs))
	}
}
