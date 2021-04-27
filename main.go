package main

import (
    "flag"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "math/rand"

    "github.com/bwmarrin/discordgo"
)

// Variables used for command line parameters
var (
    Token string
)

func init() {
    flag.StringVar(&Token, "t", "", "Bot Token")
    flag.Parse()
}

func main() {
    // Create a new Discord session using the provided bot token.
    dg, err := discordgo.New("Bot " + Token)
    if err != nil {
        fmt.Println("error creating Discord session,", err)
        return
    }

    // Register the messageCreate func as a callback for MessageCreate events.
    dg.AddHandler(messageCreate)

    // In this example, we only care about receiving message events.
    dg.Identify.Intents = discordgo.IntentsGuildMessages

    // Open a websocket connection to Discord and begin listening.
    err = dg.Open()
    if err != nil {
        fmt.Println("error opening connection,", err)
        return
    }


    /* WHAT IS THIS */
    /* does `<-sc` make the code wait for the signal.Notify() before it? That would be cool!!!!! */
    // Wait here until CTRL-C or other term signal is received.
    /*
    fmt.Println("Bot runnin'. ^C to exit.")
    for _, guild := range dg.State.Guilds {
        channels, _ := dg.GuildChannels(guild.ID)
        for _, c := range channels {
            // Check if channel is a guild text channel and not a voice or DM channel
            if c.Type != discordgo.ChannelTypeGuildText {
                continue
            }
            fmt.Println("cid %d and name %q", c.ID, c.Name)

        }
    }*/
//    dg.ChannelMessageSend();
//252302649884409859 
    sc := make(chan os.Signal, 1)
    signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
    <-sc

    // Cleanly close down the Discord session.
    dg.Close()
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
    // Ignore all messages created by the bot itself
    // This isn't required in this specific example but it's a good practice.
    if m.Author.ID == s.State.User.ID {
        return
    }

    fmt.Println(m.Author.ID);
    fmt.Println(m.Author.Email);
    fmt.Println(m.Author.Username);

    // If the message is "ping" reply with "pang"
    if m.Content == "ping" {
        s.ChannelMessageSend(m.ChannelID, "")
        fmt.Printf("%+v\n", m.Author)
    }

    if m.Content == "hey" {
        s.ChannelMessageSend(m.ChannelID, "sup")
    }

    if m.Content == "fish" {
        bassType := "Large-mouth"
        defaultMin := 20
        defaultRange := 80
        length := defaultMin + rand.Intn(defaultRange)
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(m.Author.Username, " caught a ", length, "cm ", bassType, " bass!"))
    }
}

// Bait types: fly fishing, lure - jig, lure - spoon, lure - spinner, lure -crankbait, lure - plug, powerbait - plain, powerbait - glitter, worm, minnow, crayfish, cricket, frog, offal
