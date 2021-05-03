package main

import (
    "flag"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "math/rand"
    "strings"
    "time"
    "sort"
    "io/ioutil"
    "encoding/json"
    "strings"
    "strconv"

    "github.com/bwmarrin/discordgo"
)

type Bass struct {
    Kind string
    Size int
}

const defaultMin = 20
const defaultRange = 30
const castCooldown = 3600000000000 // in nanoseconds, 1hr
//const castCooldown = 3600000000

func getBassKinds() []string {
    return []string{"Largemouth", "Smallmouth", "Spotted", "Redeye", "Shoal"}
}

var (
    Token string
    ChannelID string
    BassMap map[string][]Bass
    UserCooldowns map[string]int64
    UserCharges map[string]int
)


func init() {
    flag.StringVar(&Token, "t", "", "Bot Token")
    flag.Parse()
    ChannelID = "-1"
    BassMap = make(map[string][]Bass)
    UserCooldowns = make(map[string]int64)
    UserCharges = make(map[string]int)
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

    fmt.Println("Bot runnin'. ^C to exit.")
    for _, guild := range dg.State.Guilds {
        channels, _ := dg.GuildChannels(guild.ID)
        for _, c := range channels {
            // Check if channel is a guild text channel and not a voice or DM channel
            if c.Type != discordgo.ChannelTypeGuildText {
                continue
            }
            if c.Name == "bass-fishing" {
                ChannelID = c.ID
            }
            fmt.Println("cid %d and name %q", c.ID, c.Name)

        }
    }
    dg.ChannelMessageSend(fmt.Sprint(ChannelID), "The fishin's good!");
    load()
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
    if m.Author.ID == s.State.User.ID || m.ChannelID != ChannelID {
        return
    }

    fmt.Println(m.Author.ID);
    fmt.Println(m.Author.Email);
    fmt.Println(m.Author.Username);


    if m.Content == "hey" {
        fmt.Println(m.Author.Username + "hey")
        s.ChannelMessageSend(m.ChannelID, "sup")
    }

    if m.Content == "fish" {
        fmt.Println(m.Author.Username + " fish")
        fmt.Println(fmt.Sprintf("now %d, cooldown %d", time.Now().UnixNano(), UserCooldowns[m.Author.Username]))

        if time.Now().UnixNano() - UserCooldowns[m.Author.Username] < castCooldown {
            if (UserCharges[m.Author.Username] < 1) {
                s.ChannelMessageSend(m.ChannelID, fmt.Sprint("You can fish once per hour."))
                return
            } else {
                UserCharges[m.Author.Username] = UserCharges[m.Author.Username] - 1
            }
        }
        s1 := rand.NewSource(time.Now().UnixNano())
        r1 := rand.New(s1)
        catch := Bass{Kind: getBassKinds()[r1.Intn(len(getBassKinds()))], Size: defaultMin + r1.Intn(defaultRange)}
        BassMap[m.Author.Username] = append(BassMap[m.Author.Username], catch)
        UserCooldowns[m.Author.Username] = time.Now().UnixNano()
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(m.Author.Username, " caught a ", catch.Size, "cm ", catch.Kind, " bass!"))
        save()
    }

    if m.Content == "bass stash" {
        fmt.Println(m.Author.Username + " bass stash")
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(m.Author.Username + "'s Bass Stash: " + usersBassStashString(m.Author.Username)))
    }

    if m.Content == "leaderboard" {
        fmt.Println(m.Author.Username + " leaderboard")
        type LeaderboardBass struct {
            Name string
            Size int
            Kind string
        }
        //var allBass [3]LeaderboardBass
        allBass := make([]LeaderboardBass, 3)
        for key, basses := range BassMap {
            for _, bass := range basses {
                allBass = append(allBass, LeaderboardBass{Name: key, Size: bass.Size, Kind: bass.Kind})
            }
        }
        sort.Slice(allBass, func(i, j int) bool {
            return allBass[i].Size > allBass[j].Size
        })
        first := fmt.Sprint(":first_place: " + allBass[0].Name + "'s ", allBass[0].Size, "cm " + allBass[0].Kind + " bass.") 
        second := fmt.Sprint(":second_place: " + allBass[1].Name + "'s ", allBass[1].Size, "cm " + allBass[1].Kind + " bass.") 
        third := fmt.Sprint(":third_place: " + allBass[2].Name + "'s ", allBass[2].Size, "cm " + allBass[2].Kind + " bass.") 
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(first, "\n", second, "\n", third))
    }

    if strings.HasPrefix(m.Content, "eat") {
        tokens = strings.Split(m.Content, " ")
        if len(tokens) > 3 {
            s.ChannelMessageSend(m.ChannelID, "Never eat more than two bass at once!")
            return
        } else if len(tokens) < 3 {
            s.ChannelMessageSend(m.ChannelID, "Must specify two bass to eat.")
            return
        }

        bass1, err1 := strconv.Atoi(tokens[1])
        bass2, err2 := strconv.Atoi(tokens[2])

        if err1 != nil || err2 != nil {
            fmt.Println(fmt.Sprint("err1: ", err1, " err2: ", err2)
            return
        }

        newCharges, err := userEatBass(m.Author.Username, bass1, bass2)
        s.ChannelMessageSend(m.ChannelID, "You ate them down in one.")
    }

    if m.Content == "help" {
        fish := "**fish** - Cast your line."
        stash := "**bass stash** - List all of the fine bass you have caught."
        leaderboard := "**leaderboard** - List the top three bass."
        eat := "**eat <x1> <x2>** - Eat the chosen bass to gain energy for an extra cast. Hourly timer is not affected. Ex. `eat 7 3` eats bass number 7 and 3 as identified by `bass stash`."
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(fish, "\n", stash, "\n", leaderboard))
    }

}

func usersBassStashString(user string) string {
    var stash []string
    for i, bass := range BassMap[user] {
        stash = append(stash, fmt.Sprint("**", i + 1, "** - ", bass.Size, "cm " + bass.Kind))
    }
    return strings.Join(stash, ", ")
}

// Returns number of charges gained
func userEatBass(user string, bassIndex int) int {
    if bassIndex > len(BassMap[user]) - 1 {
        return 0, errors.New("Invalid bass index")
    }
    // Remove bass
    copy(BassMap[user][bassIndex:], BassMap[user][bassIndex + 1:])
    BassMap[user][len(BassMap[user])] - 1 = 0;
    BassMap[user] = BassMap[user][:len(BassMap[user]) - 1]
    
    newCharges := 1
    UserCharges[user] = UserCharges[user] + newCharges

    return newCharges, nil
}

func load() {
    fmt.Println("loading from file...")
    file, _ := ioutil.ReadFile("stashes.json")

    json.Unmarshal([]byte(file), &BassMap)
    fmt.Println("Load successful. Loaded BassMap:")
    for key, basses := range BassMap {
        fmt.Println(key)
        for _, bass := range basses {
            fmt.Println(fmt.Sprint("\t", bass.Kind, " ", bass.Size, "cm"))
        }
    }
}

func save() {
    file, _ := json.MarshalIndent(BassMap, "", "    ")
    _ = ioutil.WriteFile("stashes.json", file, 0644)
}



// Bait types: fly fishing, lure - jig, lure - spoon, lure - spinner, lure -crankbait, lure - plug, powerbait - plain, powerbait - glitter, worm, minnow, crayfish, cricket, frog, offal
