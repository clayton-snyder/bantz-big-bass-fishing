package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Bass struct {
	Kind string
	Size int
}

type DexEntry struct {
	Caught        bool
	LargestCaught int
	FirstCaught   time.Time
}

type WeatherInfo struct {
	Bait, Message string
}

const defaultMin, defaultRange, defaultMax = 20, 31, 69
const strongBoost, critBoost = 15, 25
const castCooldown = 3600000000000 // in nanoseconds, 1hr

func getBassKinds() map[string][]string {
	BassKinds := make(map[string][]string)
	BassKinds["Common"] = []string{"Largemouth", "Smallmouth", "Spotted", "Redeye", "Shoal"}
	BassKinds["Uncommon"] = []string{"Alabama", "Kentucky", "Florida", "Bartram's", "Choctaw", "Cahaba", "Chattahoochee", "Autstralian", "Ozark"}
	BassKinds["Rare"] = []string{"Guadalupe", "Chilean", "Japanese", "Giant"}
	BassKinds["Epic"] = []string{"Albino", "Warrior", "Strange"}
	return BassKinds
}

func getWeatherMap() map[string]WeatherInfo {
	WeatherMap := make(map[string]WeatherInfo)
	WeatherMap["drizzle"] = WeatherInfo{Bait: "plug lure", Message: "It's drizzling..."}
	WeatherMap["snow"] = WeatherInfo{Bait: "plain powerbait", Message: "Snow is falling..."}
	WeatherMap["wind"] = WeatherInfo{Bait: "jig lure", Message: "A cold wind blows..."}
	WeatherMap["storm"] = WeatherInfo{Bait: "spinner lure", Message: "The thunder rolls..."}
	WeatherMap["fog"] = WeatherInfo{Bait: "glitter powerbait", Message: "The fog is thick..."}
	WeatherMap["sun"] = WeatherInfo{Bait: "offal", Message: "The sun beats down..."}
	WeatherMap["mist"] = WeatherInfo{Bait: "fly", Message: "Mist fills the air..."}
	WeatherMap["sandstorm"] = WeatherInfo{Bait: "worm", Message: "A harsh sandstorm rages..."}
	return WeatherMap
}

func getBaitKinds() []string {
	return []string{"fly", "jig lure", "spinner lure", "plug lure", "plain powerbait", "glitter powerbait", "worm", "offal"}
}

func stringArrContains(stringArr []string, inVal string) bool {
	for _, arrVal := range stringArr {
		if inVal == arrVal {
			return true
		}
	}
	return false
}

// Returns rand # in range [min, max]. Maybe max should be exclusive, which is more of a standard
// across languages, but this is much easier to use and understand for this app's purposes, IMO.
func randInt(min int, max int) int {
	randRange := max - min + 1 // Add 1 to be inclusive of 'max' param
	return min + R.Intn(randRange)
}

var (
	Token                string
	NoGreet              bool
	GuildToBassChannelID map[string]string
	ChannelID            string
	BassMap              map[string][]Bass
	UserCooldowns        map[string]int64
	UserCharges          map[string]float32
	UserBait             map[string]int
	CurrentWeather       string
	R                    *rand.Rand
	BassKindToRarity     map[string]string
	UserDex              map[string]map[string]DexEntry
)

func init() {
	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.BoolVar(&NoGreet, "no-greet", false, "Suppress greeting message when bot comes online")
	flag.Parse()
	ChannelID = "-1"
	fmt.Printf("Parsed NoGreet as %v \n", NoGreet)
	GuildToBassChannelID = make(map[string]string)
	BassMap = make(map[string][]Bass)
	UserCooldowns = make(map[string]int64)
	UserCharges = make(map[string]float32)
	UserBait = make(map[string]int)
	CurrentWeather = "mist"
	R = rand.New(rand.NewSource(time.Now().UnixNano()))
	UserDex = make(map[string]map[string]DexEntry)

	// Load once at init to save O(n) time whenever we need to look up rarity for a Bass Kind
	// This is better than searching through map from getBassKinds() every single time, which
	// can be O(n^2) if doing for a list of Bass. This is not data duplication because the
	// 'source of truth' is still getBassKinds()
	fmt.Println("loading bass-to-rarity...")
	BassKindToRarity = make(map[string]string)
	for rarity, kinds := range getBassKinds() {
		for _, kind := range kinds {
			BassKindToRarity[kind] = rarity
		}
	}
	fmt.Println("loaded bass-to-rarity")
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
				GuildToBassChannelID[guild.ID] = c.ID
				fmt.Println(fmt.Sprintf("\tMapped guild %v to channel %v (%v)", guild.ID, c.Name, c.ID))
			}
		}
	}

	if !NoGreet {
		for guildID := range GuildToBassChannelID {
			dg.ChannelMessageSend(GuildToBassChannelID[guildID], fmt.Sprint("__New Shit__\n"+
				"**Rare bass:** New kinds of bass are in the waters. Catch 9 new uncommon bass, 4 new rare bass, and 3 new epic bass.\n"+
				"**Weather:** Weather will change every 3 hours. Check the current status with `weather`. \n"+
				"**Bait system:** Make and use bait to power up your casts. \n"+
				"* Create bait charges from bass with `make-bait <x1> <x2> ...`. Each bass grants 3 charges.\n"+
				"* Use bait by typing the bait type after `fish`. Use `bait help` for a list of bait types.\n"+
				"* Using bait increases your chances of catching a rare and/or large bass."))
		}
	}

	go func() {
		var weatherTypes []string
		for k := range getWeatherMap() {
			weatherTypes = append(weatherTypes, k)
		}
		fmt.Printf("Loaded weatherTypes from WeatherMap in anon function: %v \n", weatherTypes)
		for true {
			weatherIndex := R.Intn(len(weatherTypes))
			CurrentWeather = weatherTypes[weatherIndex]
			fmt.Println("Weather updated to: %v", CurrentWeather)
			for guildID := range GuildToBassChannelID {
				fmt.Println(guildID)
				dg.ChannelMessageSend(GuildToBassChannelID[guildID], fmt.Sprint(getWeatherMap()[CurrentWeather].Message))
			}
			time.Sleep(180 * time.Minute)
		}
	}()

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

	messageLowerCase := strings.TrimSpace(strings.ToLower(m.Content))

	if messageLowerCase == "hey" {
		fmt.Println(m.Author.Username + "hey")
		s.ChannelMessageSend(m.ChannelID, "sup")
		return
	}

	if strings.HasPrefix(messageLowerCase, "testcast") {
		tokens := strings.Split(messageLowerCase, " ")
		bass, rarity, err := cast(tokens[1])
		fmt.Println(fmt.Sprintf("bass{%v, %v cm}, rarity %v, err %v", bass.Kind, bass.Size, rarity, err))
		return
	}

	if messageLowerCase == "bait help" {
		s.ChannelMessageSend(m.ChannelID,
			fmt.Sprintf("Bait options: %v \n"+
				"Type the kind of bait you want to use after the fish command. Ex. `fish jig lure`",
				"'"+strings.Join(getBaitKinds(), "', '")+"'"))
		return
	}

	if messageLowerCase == "mario" {
		s.ChannelMessageSend(m.ChannelID, "Thank you so much for a-playing my game!")
		return
	}

	if messageLowerCase == "weather" {
		fmt.Println(fmt.Sprintf("weather %v %v", CurrentWeather, getWeatherMap()[CurrentWeather]))
		s.ChannelMessageSend(m.ChannelID, getWeatherMap()[CurrentWeather].Message)
		return
	}

	if messageLowerCase == "freefish" {
		fmt.Println(fmt.Sprintf("%v got a free cast", m.Author.Username))
		UserCharges[m.Author.Username]++
		return
	}

	if strings.HasPrefix(m.Content, "grant") {
		tokens := strings.Split(m.Content, " ")
		if m.Author.Username != "Clant" {
			return
		}

		grantee := tokens[1]
		quantity, err := strconv.Atoi(tokens[2])
		resource := tokens[3]

		if err != nil {
			return
		}

		// UserCharges, UserBait
		if grantee == "Everyone" {
			for user, _ := range UserCharges {
				if resource == "casts" {
					UserCharges[user] += float32(quantity)
				} else if resource == "bait" {
					UserBait[user] += quantity
				}
			}
		} else {
			if resource == "casts" {
				UserCharges[grantee] += float32(quantity)
			} else if resource == "bait" {
				UserBait[grantee] += quantity
			}
		}

		if len(tokens) > 4 && tokens[5] == "notify" {
			for _, channelID := range GuildToBassChannelID {
				s.ChannelMessageSend(channelID, fmt.Sprintf("%v has been granted %v %v.", grantee, quantity, resource))
			}
		}
	}

	if strings.HasPrefix(messageLowerCase, "fish") {
		scrubbed := scrubMessage(messageLowerCase)

		tokens := strings.SplitN(scrubbed, " ", 2)
		var bait string
		if len(tokens) > 1 {
			bait = tokens[1]
			if !stringArrContains(getBaitKinds(), bait) {
				fmt.Println(fmt.Sprintf("Invalid bait type '%v' used by %v.", bait, m.Author.Username))
				s.ChannelMessageSend(m.ChannelID, "Invalid bait type. Type `bait help` for a list.")
				return
			}
			if UserBait[m.Author.Username] < 1 {
				fmt.Println(fmt.Sprintf("%v tried to use bait with no charges.", m.Author.Username))
				s.ChannelMessageSend(m.ChannelID, "You don't have any bait. Embarrassing!")
				return
			}
		}

		strength := getStrengthFromBait(bait)
		fmt.Println("Got '" + strength + "' from bait: " + bait)
		caughtBass, rarity, castErr := cast(strength)

		if castErr != nil {
			fmt.Println(m.Author.Username + "'s cast failed.")
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%v", castErr))
			return
		}

		// Don't add the fish to the stash if payment is declined.
		if !debitCast(m.Author.Username, bait != "") {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("You can fish once per hour."))
			return
		}

		BassMap[m.Author.Username] = append(BassMap[m.Author.Username], caughtBass)
		save()
		s.ChannelMessageSend(m.ChannelID, catchString(m.Author.Username, caughtBass, rarity, strength))
		return
	}

	if messageLowerCase == "bass stash" {
		fmt.Println(m.Author.Username + " bass stash")
		s.ChannelMessageSend(m.ChannelID, fmt.Sprint(usersBassStashString(m.Author.Username)))
		return
	}

	if messageLowerCase == "casts" || messageLowerCase == "bait" {
		fmt.Println(m.Author.Username + " " + messageLowerCase)
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("You have %v extra casts and %v bait charges.", UserCharges[m.Author.Username], UserBait[m.Author.Username]))
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
		first := fmt.Sprint(":first_place: "+allBass[0].Name+"'s ", allBass[0].Size, "cm "+allBass[0].Kind+" bass.")
		second := fmt.Sprint(":second_place: "+allBass[1].Name+"'s ", allBass[1].Size, "cm "+allBass[1].Kind+" bass.")
		third := fmt.Sprint(":third_place: "+allBass[2].Name+"'s ", allBass[2].Size, "cm "+allBass[2].Kind+" bass.")
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
			return
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
		makeBait := "**make-bait <x1> <x2> ...** - Turn the chosen bass into bait charges to make your casts more powerful. Each bass grants 3 bait charges."
		baitHelp := "**bait help** - Display the kinds of bait."
		casts := "**casts** - Display how many cast and bait charges you have."
		weather := "**weather** - Displays the current weather."
		leaderboard := "**leaderboard** - List the top three bass."
		s.ChannelMessageSend(m.ChannelID, fmt.Sprint(fish, "\n", stash, "\n", eat, "\n", makeBait, "\n", baitHelp, "\n", casts, "\n", weather, "\n", leaderboard))
		return
	}

}

// Rolls for and returns a Bass. Doesn't do any modification or checking of user stash, charges, etc.
func cast(strength string) (Bass, string, error) {
	flip := randInt(0, 1) // flip a coin, 0 = gain extra range, 1 = don't

	var castMin, castMax int
	// Use coinflip value as a factor to decide extra range or not
	if strength == "normal" {
		castMin = defaultMin
		castMax = defaultMax - (flip * (defaultMax - (castMin + defaultRange)))
	} else if strength == "strong" {
		castMin = defaultMin + strongBoost
		castMax = defaultMax - (flip * (defaultMax - (castMin + defaultRange)))
	} else if strength == "critical" {
		castMin = defaultMin + critBoost
		castMax = defaultMax
	} else {
		fmt.Println(fmt.Sprintf("Invalid cast strength arg: %v", strength))
		return Bass{}, "", errors.New("Something went wrong with your cast.")
	}

	size := randInt(castMin, castMax)
	bassRarity := rollForRarity(strength)
	kindOptions := getBassKinds()[bassRarity]
	kind := kindOptions[randInt(0, len(kindOptions)-1)]
	bass := Bass{Size: size, Kind: kind}

	fmt.Println(fmt.Sprintf("\tflip: %v, strength: %v, castMin: %v, castMax: %v, rarity: %v, size: %v, kind: %v",
		flip, strength, castMin, castMax, bassRarity, bass.Size, bass.Kind))

	return bass, bassRarity, nil
}

// Returns bool stating if a user can fish, based on casts or cooldown.
// If true, debits a cast (and bait, if applicable) charge OR resets cooldown (if user has no charges)
func debitCast(user string, baited bool) bool {
	cast := false
	if UserCharges[user] > 0 {
		UserCharges[user]--
		cast = true
	} else if time.Now().UnixNano()-UserCooldowns[user] > castCooldown {
		UserCooldowns[user] = time.Now().UnixNano()
		cast = true
	}
	if cast && baited {
		UserBait[user]--
	}

	return cast
}

func rollForRarity(strength string) string {
	uncommonRoll, rareRoll, epicRoll := 100, 140, 150
	rarity, diceMin := "", 0
	if strength == "critical" {
		diceMin = 75
	} else if strength == "strong" {
		diceMin = 20
	}
	diceRoll := randInt(diceMin, epicRoll)
	if diceRoll < uncommonRoll {
		rarity = "Common"
	} else if diceRoll < rareRoll {
		rarity = "Uncommon"
	} else if diceRoll < epicRoll {
		rarity = "Rare"
	} else if diceRoll == epicRoll {
		rarity = "Epic"
	} else {
		rarity = "Common"
	}
	return rarity
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

func scrubMessage(input string) string {
	space := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(space.ReplaceAllString(input, " "))
}

func getStrengthFromBait(bait string) string {
	strength := "normal"
	for _, val := range getBaitKinds() {
		if bait == val {
			strength = "strong"
			break
		}
	}
	// bait != "" is to prevent auto-crit if getWeatherMap() gives a default value
	if bait != "" && bait == getWeatherMap()[CurrentWeather].Bait {
		strength = "critical"
	}

	return strength
}

func usersBassStashString(user string) string {
	rarityMap := make(map[string][]string)
	sep := " .... "
	for i, bass := range BassMap[user] {
		rarity := BassKindToRarity[bass.Kind]
		rarityMap[rarity] = append(rarityMap[rarity], fmt.Sprintf("#**%v** *%v* (%vcm)", i+1, bass.Kind, bass.Size))
	}

	stashString := fmt.Sprintf("**%v's Bass Stash**\n", user)
	stashString += fmt.Sprintf(":purple_circle: __Epic__: %v\n", strings.Join(rarityMap["Epic"], sep))
	stashString += fmt.Sprintf(":green_circle: __Rare__: %v\n", strings.Join(rarityMap["Rare"], sep))
	stashString += fmt.Sprintf(":yellow_circle: __Uncommon__: %v\n", strings.Join(rarityMap["Uncommon"], sep))
	stashString += fmt.Sprintf(":white_circle: __Common__: %v\n", strings.Join(rarityMap["Common"], sep))
	return stashString
}

func catchString(username string, caughtBass Bass, rarity string, strength string) string {
	var catchString string
	if strength == "critical" {
		catchString = ":zap: *Critical cast!* :zap:\n"
	} else if strength == "strong" {
		catchString = "Nice cast!\n"
	}

	switch rarity {
	case "Common":
		catchString += fmt.Sprintf(":white_circle: %v caught a %vcm %v bass.", username, caughtBass.Size, caughtBass.Kind)
	case "Uncommon":
		catchString += fmt.Sprintf(":yellow_circle: %v caught a %vcm %v %v bass!",
			username, caughtBass.Size, rarity, caughtBass.Kind)
	case "Rare":
		catchString += fmt.Sprintf(":green_circle: %v caught a %vcm %v %v bass!!",
			username, caughtBass.Size, rarity, caughtBass.Kind)
	case "Epic":
		catchString += fmt.Sprintf(":purple_circle: %v caught a %vcm **%v %v bass**!!",
			username, caughtBass.Size, rarity, caughtBass.Kind)
	default:
		catchString += fmt.Sprintf("%v caught a %vm %v %v bass.",
			username, caughtBass.Size, rarity, caughtBass.Kind)
	}

	return catchString
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
			return false, errors.New(fmt.Sprintf("You do not have a Bass number %v.", id))
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

func updateDex(user string, newBass Bass) bool {
	updated := false
	dexEntry := UserDex[user][newBass.Kind]
	fmt.Printf("updateDex: loaded existing(%v), new is (%v/%v)\n", dexEntry.LargestCaught, newBass.Kind, newBass.Size)

	if dexEntry.Caught {
		if dexEntry.LargestCaught > newBass.Size {
			dexEntry.LargestCaught = newBass.Size
			UserDex[user][newBass.Kind] = dexEntry
			updated = true
		}
	} else {
		dexEntry.Caught = true
		dexEntry.LargestCaught = newBass.Size
		dexEntry.FirstCaught = time.Now()
		updated = true
	}

	fmt.Printf("updateDex: newEntry is %v / %v / %v \n", dexEntry.Caught, dexEntry.LargestCaught, dexEntry.FirstCaught)
	return updated
}

func dexString(user string) string {
	// TODO
	return ""
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
