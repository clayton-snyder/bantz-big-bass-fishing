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
    "strconv"
    "errors"

    "github.com/bwmarrin/discordgo"
)

type Bass struct {
    Kind string
    Size int
}

const defaultMin = 20
const defaultRange = 31
const castCooldown = 3600000000000 // in nanoseconds, 1hr
//const castCooldown = 3600000000

func getBassKinds() []string {
    return []string{"Largemouth", "Smallmouth", "Spotted", "Redeye", "Shoal"}
}

var (
    Token string
    NoGreet bool
    GuildToBassChannelID map[string]string
    ChannelID string
    BassMap map[string][]Bass
    UserCooldowns map[string]int64
    UserCharges map[string]float32
    UserBait map[string]int
)


func init() {
    flag.StringVar(&Token, "t", "", "Bot Token")
    flag.BoolVar(&NoGreet, "no-greet", false, "Suppress greeting message when bot comes online")
    flag.Parse()
    ChannelID = "-1"
    fmt.Println("Parsed NoGreet as %v", NoGreet)
    GuildToBassChannelID = make(map[string]string)
    BassMap = make(map[string][]Bass)
    UserCooldowns = make(map[string]int64)
    UserCharges = make(map[string]float32)
    UserBait = make(map[string]int)
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



    fmt.Println("Bot runnin'. ^C to exit.")

    // For every guild the bot is in, find and map the 'bass-fishing''s channel ID
    for _, guild := range dg.State.Guilds {
        fmt.Println(fmt.Sprintf("Checking guild %v", guild.ID))
        channels, _ := dg.GuildChannels(guild.ID)
        for _, c := range channels {
            // Check if channel is a guild text channel and not a voice or DM channel
            if c.Type != discordgo.ChannelTypeGuildText {
                continue
            }
            if c.Name == "bass-fishing" {
                //ChannelID = c.ID
                GuildToBassChannelID[guild.ID] = c.ID
                fmt.Println(fmt.Sprintf("\tMapped guild %v to channel %v (%v)", guild.ID, c.Name, c.ID))
            }
            //fmt.Println("cid %d and name %q", c.ID, c.Name)
        }
    }

    if !NoGreet {
        for guildID := range GuildToBassChannelID {
            //dg.ChannelMessageSend(GuildToBassChannelID[guildID], fmt.Sprint("The fishin's good!"));
            dg.ChannelMessageSend(GuildToBassChannelID[guildID], fmt.Sprint("**Updates**\n" +
            "* You can now eat any number of bass at once. Each bass eaten grants half a charge.\n" +
            "* Added `casts` command to view the amount of extra casts stored up.\n" +
            "* Commands are now case-insensitive."))
        }
    }

    load()

    /* WHAT IS THIS */
    /* does `<-sc` make the code wait for the signal.Notify() before it? That would be cool!!!!! */
    // Wait here until CTRL-C or other term signal is received.

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
    if m.Author.ID == s.State.User.ID {
        return
    }

    fmt.Println(m.Author.ID);
    fmt.Println(m.Author.Email);
    fmt.Println(m.Author.Username);

    messageLowerCase := strings.ToLower(m.Content)

    if messageLowerCase == "hey" {
        fmt.Println(m.Author.Username + "hey")
        s.ChannelMessageSend(m.ChannelID, "sup")
        return
    }

    if messageLowerCase == "fish" {
        fmt.Println(m.Author.Username + " fish")
        fmt.Println(fmt.Sprintf("now %d, cooldown %d", time.Now().UnixNano(), UserCooldowns[m.Author.Username]))

        if time.Now().UnixNano() - UserCooldowns[m.Author.Username] < castCooldown {
            if (UserCharges[m.Author.Username] < 1.0) {
                s.ChannelMessageSend(m.ChannelID, fmt.Sprint("You can fish once per hour."))
                return
            } else {
                UserCharges[m.Author.Username] = UserCharges[m.Author.Username] - 1.0
            }
        }
        s1 := rand.NewSource(time.Now().UnixNano())
        r1 := rand.New(s1)
        catch := Bass{Kind: getBassKinds()[r1.Intn(len(getBassKinds()))], Size: defaultMin + r1.Intn(defaultRange)}
        BassMap[m.Author.Username] = append(BassMap[m.Author.Username], catch)
        UserCooldowns[m.Author.Username] = time.Now().UnixNano()
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(m.Author.Username, " caught a ", catch.Size, "cm ", catch.Kind, " bass!"))
        save()
        return
    }

    if messageLowerCase == "bass stash" {
        fmt.Println(m.Author.Username + " bass stash")
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(m.Author.Username + "'s Bass Stash: " + usersBassStashString(m.Author.Username)))
        return
    }

    if messageLowerCase == "casts" {
        fmt.Println(m.Author.Username + " casts")
        s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("You have %v extra casts.", UserCharges[m.Author.Username]))
        return
    }

    if messageLowerCase == "leaderboard" {
        fmt.Println(m.Author.Username + " leaderboard")
        type LeaderboardBass struct {
            Name string
            Size int
            Kind string
        }
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
        return
    }

    if strings.HasPrefix(messageLowerCase, "eat") {
        fmt.Println(m.Author.Username + " eat")
        tokens := strings.Split(messageLowerCase, " ")
        bassIds, strParseErr := stringSliceToInt(tokens[1:]) // Ignore first element (the command string)

        if strParseErr != nil {
            fmt.Println(fmt.Sprintf("%v", strParseErr))
            s.ChannelMessageSend(m.ChannelID, fmt.Sprint("Wrong."))
            return
        }

        gainedCharges, err := userEatBass(m.Author.Username, bassIds)
        if err != nil {
            s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%v", err))
            return
        }

        s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("You ate them down in one. Gained %v casts.", gainedCharges))
        return
    }

    if strings.HasPrefix(messageLowerCase, "make-bait") {
        fmt.Println(m.Author.Username + " make-bait")
        bassIds, parseErr := stringSliceToInt(strings.Split(messageLowerCase, " ")[1:]) // Ignore first element (the command string)
        if parseErr != nil {
            fmt.Println(fmt.Sprintf("%v got error: %v", m.Author.Username, parseErr))
            s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%v", parseErr))
        }

        gainedCharges, makeBaitErr := userMakeBait(m.Author.Username, bassIds)
        if makeBaitErr != nil {
            s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%v", makeBaitErr))
            return
        }

        s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Gained %v bait charges.", gainedCharges))
        save()
        return
    }

    if messageLowerCase == "help" {
        fmt.Println(m.Author.Username + " help")
        fish := "**fish** - Cast your line."
        stash := "**bass stash** - List all of the fine bass you have caught."
        eat := "**eat <x1> <x2> ...** - Eat the chosen bass to gain energy for an extra cast. Gain 0.5 casts for every bass. Hourly timer is not affected. *Ex.* `eat 7 3 4` eats bass numbers 7, 3, and 4 as identified by `bass stash` and grants 1.5 extra casts."
        makeBait := "**make-bait <x1> <x2> ...** - Turn the chosen bass into bait charges to power up your casts. Each bass grants 3 bait charges."
        casts := "**casts** - Display how many extra casts you have."
        leaderboard := "**leaderboard** - List the top three bass."
        s.ChannelMessageSend(m.ChannelID, fmt.Sprint(fish, "\n", stash, "\n", eat, "\n", makeBait, "\n", casts, "\n", leaderboard))
        return
    }

}

func stringSliceToInt(stringSlice []string) ([]int, error) {
    var intSlice []int

    for _, str := range stringSlice {
        intVal, err := strconv.Atoi(str)
        if err != nil {
            return nil, err
        }
        intSlice = append(intSlice, intVal)
    }

    return intSlice, nil
}

func usersBassStashString(user string) string {
    var stash []string
    for i, bass := range BassMap[user] {
        stash = append(stash, fmt.Sprint("**", i + 1, "** - ", bass.Size, "cm " + bass.Kind))
    }
    return strings.Join(stash, ", ")
}

// Returns cast charges gained
func userEatBass(user string, bassIds []int) (float32, error) {
    _, bassIdErr := validateBassIdList(user, bassIds)
    if bassIdErr != nil {
        fmt.Println(fmt.Sprintf("%v got error eating bass: %v", user, bassIdErr))
        return 0.0, bassIdErr
    }

    // Mark specified Bass for deletion, incrementing charge for each
    var newCharges float32
    for _, id := range bassIds {
        index := id - 1 // Adjust for 0 indexing
        BassMap[user][index].Kind = "DELETE"
        newCharges += 0.5
    }

    BassMap[user] = collapseStash(user)
    UserCharges[user] = UserCharges[user] + newCharges

    return newCharges, nil
}

// Returns bait charges gained
func userMakeBait(user string, bassIds []int) (int, error) {
    _, bassIdErr := validateBassIdList(user, bassIds)
    if bassIdErr != nil {
        fmt.Println(bassIdErr)
        return 0, errors.New(fmt.Sprintf("%v", bassIdErr))
    }

    var newBait int
    for _, id := range bassIds {
        index := id - 1 // Adjust for 0 indexing
        BassMap[user][index].Kind = "DELETE"
        newBait += 3
    }

    BassMap[user] = collapseStash(user)
    UserBait[user] = UserBait[user] + newBait

    return newBait, nil
}

// Validates that a given list of bassIds are all clean and match a bass the user has.
func validateBassIdList(user string, bassIds []int) (bool, error) {
    for _, id := range bassIds {
        if id < 1 || id > len(BassMap[user]) {
            return false, errors.New(fmt.Sprintf("Bass ID too low (minimum is 1): %v", id))
        }
    }

    return true, nil
}

// Removes all Bass marked for deletion
func collapseStash(user string) []Bass {
    var collapsed = []Bass{}

    for _, bass := range BassMap[user] {
        if bass.Kind != "DELETE" {
            collapsed = append(collapsed, bass)
        }
    }

    return collapsed
}

func load() {
    fmt.Println("loading stashes from file...")
    stashesFile, _ := ioutil.ReadFile("stashes.json")

    json.Unmarshal([]byte(stashesFile), &BassMap)
    fmt.Println("Stashes load successful. Loaded BassMap:")
    for key, basses := range BassMap {
        fmt.Println(key)
        for _, bass := range basses {
            fmt.Println(fmt.Sprint("\t", bass.Kind, " ", bass.Size, "cm"))
        }
    }

    fmt.Println("loading bait charges from file...")
    baitFile, _ := ioutil.ReadFile("bait_charges.json")

    json.Unmarshal([]byte(baitFile), &UserBait)
    fmt.Println("Bait charges load successful. Loaded charges:")
    for key, charges := range UserBait {
        fmt.Println(fmt.Sprintf("%v: %v", key, charges))
    }
}

func save() {
    stashFile, _ := json.MarshalIndent(BassMap, "", "    ")
    _ = ioutil.WriteFile("stashes.json", stashFile, 0644)
    baitFile, _ := json.MarshalIndent(UserBait, "", "    ")
    _ = ioutil.WriteFile("bait_charges.json", baitFile, 0644)
}



// Bait types: fly fishing, lure - jig, lure - spoon, lure - spinner, lure -crankbait, lure - plug, powerbait - plain, powerbait - glitter, worm, minnow, crayfish, cricket, frog, offal
