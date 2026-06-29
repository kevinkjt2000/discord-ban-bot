package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

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

func handleMessage(s *discordgo.Session, honeypotID string, m *discordgo.MessageCreate) {
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

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("failed to create discord session: %v", err)
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		handleMessage(s, honeypotID, m)
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

	log.Printf("ban-bot connected. monitoring honeypot channel %s", honeypotID)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Println("shutting down")
}
