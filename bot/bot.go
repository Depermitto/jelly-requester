package bot

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strings"

	"github.com/bwmarrin/discordgo"
	bolt "go.etcd.io/bbolt"
)

const guildID = ""

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "help",
		Description: "Let me help you",
	},
	{
		Name:        "list",
		Description: "Show currently pending requests",
	},
	{
		Name:        "request",
		Description: "Request a piece of media - a movie, show or piece of music",
		Options: []*discordgo.ApplicationCommandOption{{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "link",
			Description: "Link to movie, show or music",
			Required:    true,
		}}},
	{
		Name:        "drop",
		Description: "Pull back a pending request",
		Options: []*discordgo.ApplicationCommandOption{{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "request",
			Description: "Index to a pending request",
			Required:    true,
		}}},
	{
		Name:        "mark-done",
		Description: "Assert that media has been added",
		Options: []*discordgo.ApplicationCommandOption{{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "request",
			Description: "Index to a pending request",
			Required:    true,
		}}},
}

func Run(token string, db *bolt.DB) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalln("Bad token")
	}

	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v\n", s.State.User.Username, s.State.User.Discriminator)
	})
	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.ApplicationCommandData().Name {
		case "help":
			respond(s, i, "You do not need help", 0)
		case "list":
			var response string
			err := db.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("media"))
				idx := 1
				return b.ForEach(func(link []byte, pending []byte) error {
					if pending[0] == 1 {
						response += fmt.Sprintf("%d. <%s>\n", idx, link)
						idx++
					}
					return nil
				})
			})
			if len(response) == 0 {
				respond(s, i, "No pending media", 0)
			} else if err != nil {
				respond(s, i, "Internal server error", 0)
			} else {
				respond(s, i, "Pending media:\n"+response, 0)
			}
		case "request":
			options := i.ApplicationCommandData().Options
			rawUrl := options[0].StringValue()
			rawUrl = strings.ReplaceAll(rawUrl, "www.", "")
			if !strings.HasPrefix(rawUrl, "http") {
				rawUrl = "https://" + rawUrl
			}

			badCharacters := regexp.MustCompile(`[\s<>'"\x00-\x1F|{}\[\]^]`).MatchString(rawUrl)
			link, err := url.Parse(rawUrl)
			badScheme := link.Scheme != "https" && link.Scheme != "http"
			if err != nil || badCharacters || badScheme {
				respond(s, i, fmt.Sprintf("Bad link: *%s*", link), discordgo.MessageFlagsEphemeral)
				return
			}

			domains := []string{"thetvdb.com", "imdb.com", "themoviedb.org", "discogs.com", "open.spotify.com", "musicbrainz.org"}
			if !slices.Contains(domains, link.Hostname()) {
				respond(s, i, "Unsupported domain, provide a link within one of these: "+strings.Join(domains, ", "), discordgo.MessageFlagsEphemeral)
				return
			}

			log.Printf("%s#%s requested %v\n", i.Member.User.Username, i.Member.User.ID, link)
			err = db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("media"))
				return b.Put([]byte(link.String()), []byte{1})
			})
			if err != nil {
				respond(s, i, "Internal server error", 0)
				return
			}
			respond(s, i, fmt.Sprintf("Accepted %v, I will get right to it 🫡", link), 0)
		case "drop":
			options := i.ApplicationCommandData().Options
			request := options[0].IntValue()

			var keyToDelete []byte
			err := db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("media"))
				c := b.Cursor()

				idx := int64(1)
				for link, pending := c.First(); link != nil; link, pending = c.Next() {
					if pending[0] == 1 {
						if request == idx {
							keyToDelete = link
						}
						idx++
					}
				}

				return b.Delete(keyToDelete)
			})
			if keyToDelete == nil {
				respond(s, i, "No media found", discordgo.MessageFlagsEphemeral)
			} else if err != nil {
				respond(s, i, "Internal server error", 0)
			} else {
				respond(s, i, fmt.Sprintf("<%s> dropped", keyToDelete), 0)
			}
		case "mark-done":
			options := i.ApplicationCommandData().Options
			request := options[0].IntValue()

			var keyToMarkDone []byte
			err := db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("media"))
				c := b.Cursor()

				idx := int64(1)
				for link, pending := c.First(); link != nil; link, pending = c.Next() {
					if pending[0] == 1 {
						if request == idx {
							keyToMarkDone = link
						}
						idx++
					}
				}

				return b.Put(keyToMarkDone, []byte{0})
			})
			if keyToMarkDone == nil {
				respond(s, i, "No media found", discordgo.MessageFlagsEphemeral)
			} else if err != nil {
				respond(s, i, "Internal server error", 0)
			} else {
				respond(s, i, fmt.Sprintf("<%s> marked done", keyToMarkDone), 0)
			}
		}
	})

	err = s.Open()
	if err != nil {
		log.Fatalf("Cannot open session: %v\n", err)
	}
	defer s.Close()

	for _, command := range commands {
		_, err = s.ApplicationCommandCreate(s.State.User.ID, guildID, command)
		if err != nil {
			log.Fatalf("Cannot register command %v\n", command.Name)
		}
	}
	
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	log.Println("Removing commands...")
	registeredCommands, err := s.ApplicationCommands(s.State.User.ID, guildID)
	if err != nil {
		log.Fatalf("Could not fetch registered commands: %v", err)
	}

	for _, v := range registeredCommands {
		err := s.ApplicationCommandDelete(s.State.User.ID, guildID, v.ID)
		if err != nil {
			log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
		}
	}
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, msg string, flags discordgo.MessageFlags) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   flags,
		},
	})
}
