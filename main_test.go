package main

import (
	"testing"

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
