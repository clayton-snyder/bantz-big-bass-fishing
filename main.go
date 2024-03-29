/*
 * PATCH NOTES:
 * Eating uncommon/rare/epic grants more because you give up more
 * Cap raised by a lot but very rare to catch very high stuff
 *
 */

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
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

type Trophy struct {
	Title            string
	Points           int
	PointDescriptor  string
	Champs           []string
	Record           int
	GetDisplayString func() string
}

type DexEntry struct {
	Caught        bool
	LargestCaught int
	FirstCaught   time.Time
}

type WeatherInfo struct {
	Bait, Message string
}

type QuotesApiResponse struct {
	Contents Contents
	Success  string
}

type Contents struct {
	Quotes []Quote
}

type Quote struct {
	Author string
	Quote  string
	Id     string
	Image  string
	Length int
}

const defaultMin, defaultRange, defaultMax = 20, 31, 75
const strongBoost, critBoost = 15, 25
const castCooldown = 3600000000000 // in nanoseconds, 1hr
const layoutUS = "January 2, 2006"

const maxMessageLength = 1750

func getBassKinds() map[string][]string {
	BassKinds := make(map[string][]string)
	BassKinds["Epic"] = []string{"Albino", "Warrior", "Strange"}
	BassKinds["Rare"] = []string{"Guadalupe", "Chilean", "Japanese", "Giant"}
	BassKinds["Uncommon"] = []string{"Alabama", "Kentucky", "Florida", "Bartram's", "Choctaw", "Cahaba", "Chattahoochee", "Autstralian", "Ozark"}
	BassKinds["Common"] = []string{"Largemouth", "Smallmouth", "Spotted", "Redeye", "Shoal"}
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

func isTimeForAQuote() bool {
	lastQuoteTime, err := readLastSportsQuoteDate()
	if err != nil {
		fmt.Printf("Error getting last quote date: %v", err)
		return false
	}

	return lastQuoteTime.Add(time.Hour * 24).Before(time.Now())
}

func getSportsQuote() string {
	response, err := http.Get("https://quotes.rest/qod?category=sports&language=en")
	if err != nil {
		fmt.Printf("Error with sending request: %v", err)
		return "We got a serious problem."
	}

	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Printf("Error with ReadAll body: %v", err)
		return "We got a serious problem."
	}

	var jsonBody QuotesApiResponse
	json.Unmarshal(body, &jsonBody)

	return fmt.Sprintf("\"%v\"\n\n%v", jsonBody.Contents.Quotes[0].Quote, jsonBody.Contents.Quotes[0].Author)
}

// Returns an abbreviated version of the passed-in string so that it is under 'limit' size.
func abbreviateString(s string, limit int) string {
	if len(s) < limit {
		return s
	}

	abbreviator := " ** *[too large, abbreviated...]* **"
	abbrevString := s[0 : limit-len(abbreviator)]
	abbrevString += abbreviator

	return abbrevString
}

// This is just to be run one time to populate Dexes from current stash. No need to keep it after
// the first time Dexes are deployed.
func loadBassDexes() {
	for user, stash := range BassMap {
		for _, bass := range stash {
			updateDex(user, bass)
		}
	}
	save()
	fmt.Println("Loaded. Here's the dexes...")
	for user, dex := range UserDex {
		fmt.Printf("%v: %v \n", user, dex)
	}
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
	SportsChannelID      string
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
			if c.Name == "sports-motivation" {
				SportsChannelID = c.ID
				fmt.Printf("Found sports-motivation channel ID: %v", SportsChannelID)
			}
		}
	}

	if !NoGreet {
		for guildID := range GuildToBassChannelID {
			dg.ChannelMessageSend(GuildToBassChannelID[guildID], fmt.Sprint("__Update: New Leaderboard (beta)__\n"+
				"Fishing is about more than just \"catch long bass\". It is now also about \"catch rare bass\", \"collect many kinds of bass\", and \"hoard large quantity of bass\".\n"+
				"Four trophies can now be won, each granting varying trophy points. Having the most trophy points makes you the World Heavyweight Champion.\n"+
				"Multiple anglers can hold a trophy at one time and each will receive full points. But there can only be one Champion, so trophy point ties are broken by total stash length.\n"+
				"This might be buggy for now because tbh I did not test it very much."))
		}
	}

	go func() {
		var weatherTypes []string
		for k := range getWeatherMap() {
			weatherTypes = append(weatherTypes, k)
		}
		fmt.Printf("Loaded weatherTypes from WeatherMap in anon function: %v \n", weatherTypes)
		for {
			weatherIndex := R.Intn(len(weatherTypes))
			CurrentWeather = weatherTypes[weatherIndex]
			fmt.Printf("Weather updated to: %v\n", CurrentWeather)
			for guildID := range GuildToBassChannelID {
				fmt.Println(guildID)
				dg.ChannelMessageSend(GuildToBassChannelID[guildID], fmt.Sprint(getWeatherMap()[CurrentWeather].Message))
			}
			time.Sleep(180 * time.Minute)
		}
	}()

	go func() {
		for {
			if isTimeForAQuote() {
				quote := getSportsQuote()
				dg.ChannelMessageSend(SportsChannelID, getSportsQuote())
				if quote != "We got a serious problem." {
					updateLastSportsQuoteDate()
				}
			} else {
				fmt.Printf("%v: Not time for a quote.\n", time.Now().Format(time.RFC3339))
			}
			time.Sleep(time.Minute)
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
	channelID := GuildToBassChannelID[m.GuildID]

	// Don't respond to messages sent in channels other than #bass-fishing
	if m.ChannelID != channelID {
		fmt.Printf("%v != %v", m.ChannelID, channelID)
		return
	}

	if strings.HasPrefix(messageLowerCase, "spam") {
		if m.Author.Username != "Clant" {
			s.ChannelMessageSend(channelID, fmt.Sprintf("Who do you think you are?"))
			return
		}

		tokens := strings.Split(messageLowerCase, " ")
		if len(tokens) < 2 {
			return
		}

		count, _ := strconv.Atoi(tokens[1])
		repeatedString := strings.Repeat("s", count)

		s.ChannelMessageSend(channelID, repeatedString)
	}

	if messageLowerCase == "loaddex" {
		loadBassDexes()
		fmt.Println("Loaded.")
		return
	}

	if messageLowerCase == "hey" {
		fmt.Println(m.Author.Username + "hey")
		s.ChannelMessageSend(channelID, "sup")
		return
	}

	if strings.HasPrefix(messageLowerCase, "testcast") {
		tokens := strings.Split(messageLowerCase, " ")
		bass, rarity, err := cast(tokens[1])
		fmt.Println(fmt.Sprintf("bass{%v, %v cm}, rarity %v, err %v", bass.Kind, bass.Size, rarity, err))
		return
	}

	if messageLowerCase == "bait help" {
		s.ChannelMessageSend(channelID,
			fmt.Sprintf("Bait options: %v \n"+
				"Type the kind of bait you want to use after the fish command. Ex. `fish jig lure`",
				"'"+strings.Join(getBaitKinds(), "', '")+"'"))
		return
	}

	if strings.HasPrefix(messageLowerCase, "mario") {
		s.ChannelMessageSend(channelID, "Thank you so much for a-playing my game!")
		return
	}

	if messageLowerCase == "weather" {
		fmt.Println(fmt.Sprintf("weather %v %v", CurrentWeather, getWeatherMap()[CurrentWeather]))
		s.ChannelMessageSend(channelID, getWeatherMap()[CurrentWeather].Message)
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
			fmt.Printf("Error granting casts: %v", err)
			return
		}

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

		if len(tokens) > 4 && tokens[4] == "notify" {
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
				s.ChannelMessageSend(channelID, "Invalid bait type. Type `bait help` for a list.")
				return
			}
			if UserBait[m.Author.Username] < 1 {
				fmt.Println(fmt.Sprintf("%v tried to use bait with no charges.", m.Author.Username))
				s.ChannelMessageSend(channelID, "You don't have any bait. Embarrassing!")
				return
			}
		}

		strength := getStrengthFromBait(bait)
		fmt.Println("Got '" + strength + "' from bait: " + bait)
		caughtBass, rarity, castErr := cast(strength)
		if needsBirthdayBass(m.Author.Username) {
			caughtBass.Kind = "Birthday"
		}

		if castErr != nil {
			fmt.Println(m.Author.Username + "'s cast failed.")
			s.ChannelMessageSend(channelID, fmt.Sprintf("%v", castErr))
			return
		}

		// Don't add the fish to the stash if payment is declined.
		if !debitCast(m.Author.Username, bait != "") {
			s.ChannelMessageSend(channelID, fmt.Sprintf("You can fish once per hour."))
			return
		}

		BassMap[m.Author.Username] = append(BassMap[m.Author.Username], caughtBass)
		updateDex(m.Author.Username, caughtBass)
		save()
		s.ChannelMessageSend(channelID, catchString(m.Author.Username, caughtBass, rarity, strength))
		return
	}

	if strings.HasPrefix(messageLowerCase, "stash") || strings.HasPrefix(messageLowerCase, "bass stash") || strings.HasPrefix(messageLowerCase, "stash") {
		fmt.Println(m.Author.Username + " " + m.Content)
		tokens := strings.SplitN(scrubMessage(m.Content), " ", 2)
		user := m.Author.Username
		if len(tokens) > 1 {
			user = tokens[1]
		}

		if len(BassMap[user]) == 0 {
			s.ChannelMessageSend(channelID, fmt.Sprintf("No user named '%v' has a stash.", user))
			return
		}

		s.ChannelMessageSend(channelID, abbreviateString(usersBassStashString(user), maxMessageLength))
		return
	}

	if strings.HasPrefix(messageLowerCase, "bassdex") || strings.HasPrefix(messageLowerCase, "dex") {
		fmt.Println(m.Author.Username + " " + m.Content)
		tokens := strings.SplitN(scrubMessage(m.Content), " ", 2)
		user := m.Author.Username
		if len(tokens) > 1 {
			user = tokens[1]
		}

		if len(UserDex[user]) == 0 {
			s.ChannelMessageSend(channelID, fmt.Sprintf("No user named '%v' has caught a bass.", user))
			return
		}

		s.ChannelMessageSend(channelID, abbreviateString(dexString(user), maxMessageLength))
		return
	}

	if messageLowerCase == "casts" || messageLowerCase == "bait" {
		fmt.Println(m.Author.Username + " " + messageLowerCase)
		s.ChannelMessageSend(channelID, fmt.Sprintf("You have %v extra casts and %v bait charges.", UserCharges[m.Author.Username], UserBait[m.Author.Username]))
		return
	}

	if messageLowerCase == "leaderboard" {
		fmt.Println(m.Author.Username + " leaderboard")
		trophyCase := getTrophyCase()

		var bigChamp string
		var subChamps strings.Builder
		for _, trophy := range trophyCase {
			fmt.Printf("title:%v, champs:%v, points:%v, pointDescriptor:%v, record:%v\n",
				trophy.Title, trophy.Champs, trophy.Points, trophy.PointDescriptor, trophy.Record)

			if trophy.Title == "World Heavyweight Champion" {
				// There should only ever be one big champ
				bigChamp = fmt.Sprintf(":crown::medal: __%v__ :medal::crown:\n**World Heavyweight Champion**", trophy.Champs[0])
			} else {
				subChamps.WriteString(trophy.GetDisplayString() + "\n")
			}
		}

		s.ChannelMessageSend(channelID, abbreviateString(bigChamp+"\n\n"+subChamps.String(), maxMessageLength))
	}

	if messageLowerCase == "oldleaderboard" {
		fmt.Println(m.Author.Username + " oldleaderboard")
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
		s.ChannelMessageSend(channelID, fmt.Sprint(first, "\n", second, "\n", third))
		return
	}

	if strings.HasPrefix(messageLowerCase, "eat") {
		fmt.Println(m.Author.Username + " eat")
		tokens := strings.Split(messageLowerCase, " ")
		bassIds, strParseErr := stringSliceToInt(tokens[1:]) // Ignore first element (the command string)

		if strParseErr != nil {
			fmt.Println(fmt.Sprintf("%v", strParseErr))
			s.ChannelMessageSend(channelID, fmt.Sprint("Wrong."))
			return
		}

		gainedCharges, err := userEatBass(m.Author.Username, bassIds)
		if err != nil {
			s.ChannelMessageSend(channelID, fmt.Sprintf("%v", err))
			return
		}

		s.ChannelMessageSend(channelID, fmt.Sprintf("You ate them down in one. Gained %v casts.", gainedCharges))
		return
	}

	if strings.HasPrefix(messageLowerCase, "make-bait") {
		fmt.Println(m.Author.Username + " make-bait")
		bassIds, parseErr := stringSliceToInt(strings.Split(messageLowerCase, " ")[1:]) // Ignore first element (the command string)
		if parseErr != nil {
			fmt.Println(fmt.Sprintf("%v got error: %v", m.Author.Username, parseErr))
			s.ChannelMessageSend(channelID, fmt.Sprintf("%v", parseErr))
			return
		}

		gainedCharges, makeBaitErr := userMakeBait(m.Author.Username, bassIds)
		if makeBaitErr != nil {
			s.ChannelMessageSend(channelID, fmt.Sprintf("%v", makeBaitErr))
			return
		}

		s.ChannelMessageSend(channelID, fmt.Sprintf("Gained %v bait charges.", gainedCharges))
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
		dex := "**bassdex <user>** - Displays the BassDex of the given user. Leave out <user> to display your own."
		leaderboard := "**leaderboard** - List the top three bass."
		s.ChannelMessageSend(channelID, fmt.Sprint(fish, "\n", stash, "\n", eat, "\n", makeBait, "\n", baitHelp, "\n", casts, "\n", weather, "\n", dex, "\n", leaderboard))
		return
	}
}

func dateWithinDaysRange(dateString string, days int) (bool, error) {
	dateStamp, err := time.Parse("01/02/2006", dateString)
	if err != nil {
		return false, err
	}

	today := time.Now()
	prevBound := today.AddDate(0, 0, 0-days)
	nextBound := today.AddDate(0, 0, days)

	withinRange := dateStamp.After(prevBound) && dateStamp.Before(nextBound)
	fmt.Printf("prev:%v, next:%v, arg:%v, within? %v\n", prevBound, nextBound, dateStamp, withinRange)
	// fmt.Printf("%v within %v days of today (%v) ? = %v\n", dateString, days, today, withinRange)

	return withinRange, nil
}

func needsBirthdayBass(user string) bool {
	isBday, err := dateWithinDaysRange(getUserBirthday(user), 2)
	if err != nil {
		fmt.Printf("Error checking %v birthday bass: %v\n", user, err)
	}

	// Only one bday bass in stash at one time
	for _, bass := range BassMap[user] {
		if bass.Kind == "Birthday" {
			return false
		}
	}

	return isBday
}

// Returns the birthday date string with the current year (for comparison to time.Now())
func getUserBirthday(user string) string {
	var monthDay string
	switch user {
	case "Forest":
		monthDay = "07/14"
	case "expnch":
		monthDay = "10/26"
	case "Nolan":
		monthDay = "12/18"
	case "KaiserSose":
		monthDay = "01/13"
	case "nocturne":
		monthDay = "05/22"
	case "Clant":
		monthDay = "05/23"
	}

	return fmt.Sprintf("%v/%v", monthDay, time.Now().Year())
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
		fmt.Println(fmt.Sprintf("Invalid cast strength arg: %v\n", strength))
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
	if time.Now().UnixNano()-UserCooldowns[user] > castCooldown {
		UserCooldowns[user] = time.Now().UnixNano()
		cast = true
	} else if UserCharges[user] >= 1.0 {
		UserCharges[user]--
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

// Array of trophies including World Heavyweight Champ, which is calculated based on sub-trophies
func getTrophyCase() []Trophy {
	trophies := []Trophy{}
	trophies = append(trophies, getChampCollection())
	trophies = append(trophies, getChampHoarding())
	trophies = append(trophies, getChampLongBass())
	trophies = append(trophies, getChampTastefulStash())

	champPoints := make(map[string]int)

	// Calculate every user's total champion points based on the individual trophies they own
	for _, trophy := range trophies {
		for _, user := range trophy.Champs {
			champPoints[user] += trophy.Points
		}
	}

	// Find the user(s) with the highest champion points
	highChampPoints, highUsers := -1, []string{}
	for user, points := range champPoints {
		if points > highChampPoints {
			highUsers = nil
			highUsers = append(highUsers, user)
			highChampPoints = points
		} else if points == highChampPoints {
			highUsers = append(highUsers, user)
		}
	}

	// If there is a tie for highest champion points, user with the most length of basses wins.
	if len(highUsers) > 1 {
		// Sudden Death!
		tiebreakWinner := highUsers[0]
		longestStashTotal := -1
		for _, tiedUser := range highUsers {
			userTotal := 0
			for _, bass := range BassMap[tiedUser] {
				userTotal += bass.Size
			}
			if userTotal > longestStashTotal {
				longestStashTotal = userTotal
				tiebreakWinner = tiedUser
			}
		}

		// This is because Trophy object takes an array for Champs since individual trophies can have multiple holders.
		// But WHC will always be one person. If there was no tiebreak it will already be set correctly.
		highUsers = []string{tiebreakWinner}
	}

	heavyweightTrophy := Trophy{Title: "World Heavyweight Champion", Champs: highUsers, Record: highChampPoints}
	trophies = append(trophies, heavyweightTrophy)
	return trophies
}

func champArrString(champs []string) string {
	return "**" + strings.Join(champs[0:len(champs)-1], "**, **") + "** and **" + champs[len(champs)-1] + "**"
}

func getChampCollection() Trophy {
	trophyPoints := 2

	high := -1
	var champs []string
	for user, dex := range UserDex {
		if len(dex) > high {
			high = len(dex)
			champs = nil
			champs = append(champs, user)
		} else if len(dex) == high {
			champs = append(champs, user)
		}
	}

	return Trophy{
		Title:           "Collection",
		Champs:          champs,
		Points:          trophyPoints,
		PointDescriptor: "BassDex entries",
		Record:          high,
		GetDisplayString: func() string {
			if len(champs) > 1 {
				champStr := champArrString(champs)
				return fmt.Sprintf(":trophy:(%v) %v, the *Champions of Collection*, each have %v unique Bass types",
					trophyPoints, champStr, high)
			}
			return fmt.Sprintf(":trophy:(%v) **%v**, the *Champion of Collection*, has %v unique Bass types.",
				trophyPoints, champs[0], high)
		},
	}
}

func getChampHoarding() Trophy {
	trophyPoints := 1

	high := -1
	var champs []string
	for user, stash := range BassMap {
		if len(stash) > high {
			high = len(stash)
			champs = nil
			champs = append(champs, user)
		} else if len(stash) == high {
			champs = append(champs, user)
		}
	}

	return Trophy{
		Title:           "Hoarding",
		Champs:          champs,
		Points:          trophyPoints,
		PointDescriptor: "total Bass",
		Record:          high,
		GetDisplayString: func() string {
			if len(champs) > 1 {
				champStr := champArrString(champs)
				return fmt.Sprintf(":trophy:(%v) %v, the *Champions of Hoarding*, each have %v total Bass",
					trophyPoints, champStr, high)
			}
			return fmt.Sprintf(":trophy:(%v) **%v**, the *Champion of Hoarding*, has %v total Bass.",
				trophyPoints, champs[0], high)
		},
	}
}

func getChampLongBass() Trophy {
	trophyPoints := 2

	high := -1
	var champs []string
	for user, stash := range BassMap {
		for _, bass := range stash {
			if bass.Size > high {
				high = bass.Size
				champs = nil
				champs = append(champs, user)
			} else if bass.Size == high {
				if !stringArrContains(champs, user) {
					champs = append(champs, user)
				}
			}
		}
	}

	return Trophy{
		Title:           "Long Bass",
		Champs:          champs,
		Points:          trophyPoints,
		PointDescriptor: "cm",
		Record:          high,
		GetDisplayString: func() string {
			if len(champs) > 1 {
				champStr := champArrString(champs)
				return fmt.Sprintf(":trophy:(%v) %v, the *Champions of Long Bass*, each have a Bass of the long length %vcm.",
					trophyPoints, champStr, high)
			}
			return fmt.Sprintf(":trophy:(%v) **%v**, the *Champion of Long Bass*, has a Bass of the long length %vcm.",
				trophyPoints, champs[0], high)
		},
	}
}

func getChampTastefulStash() Trophy {
	trophyPoints := 3

	high := -1
	var champs []string
	for user, _ := range BassMap {
		score := getRarityScore(user)
		if score > high {
			high = score
			champs = nil
			champs = append(champs, user)
		} else if score == high {
			champs = append(champs, user)
		}
	}

	return Trophy{
		Title:           "Tasteful Stash",
		Champs:          champs,
		Points:          trophyPoints,
		PointDescriptor: "rarity points",
		Record:          high,
		GetDisplayString: func() string {
			if len(champs) > 1 {
				champStr := champArrString(champs)
				return fmt.Sprintf(":trophy:(%v) %v, the *Champions of Tasteful Stash*, each have %v rarity points.",
					trophyPoints, champStr, high)
			}
			return fmt.Sprintf(":trophy:(%v) **%v**, the *Champion of Tasteful Stash*, has %v rarity points.",
				trophyPoints, champs[0], high)
		},
	}
}

func getRarityScore(user string) int {
	ptsEpic, ptsRare, ptsUncommon := 5, 2, 1 // TODO: Revisit this after adjusting rarity scales
	stash, score := BassMap[user], 0
	for _, bass := range stash {
		switch BassKindToRarity[bass.Kind] {
		case "Epic":
			score += ptsEpic
		case "Rare":
			score += ptsRare
		case "Uncommon":
			score += ptsUncommon
		}
	}
	return score
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

	if caughtBass.Kind == "Birthday" {
		catchString = fmt.Sprintf(":birthday: %v caught a %vcm **%v bass!**", username, caughtBass.Size, caughtBass.Kind)
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
	fmt.Printf("updateDex for %v: loaded existing(%v), new is (%v/%v)\n", user, dexEntry.LargestCaught, newBass.Kind, newBass.Size)

	if dexEntry.Caught {
		if newBass.Size > dexEntry.LargestCaught {
			dexEntry.LargestCaught = newBass.Size
			UserDex[user][newBass.Kind] = dexEntry
			updated = true
		}
	} else {
		dexEntry.Caught = true
		dexEntry.LargestCaught = newBass.Size
		dexEntry.FirstCaught = time.Now()

		// This is necessary due to Go weirdness; apparently "A nil map behaves like an empty map when reading,
		// but attempts to write to a nil map will cause a runtime panic". https://blog.golang.org/maps
		// So if this a user's first DexEntry, we have to make() it first
		if len(UserDex[user]) == 0 {
			UserDex[user] = make(map[string]DexEntry)
		}
		UserDex[user][newBass.Kind] = dexEntry
		updated = true
	}

	fmt.Printf("updateDex: newEntry is %v / %v / %v \n", dexEntry.Caught, dexEntry.LargestCaught, dexEntry.FirstCaught)
	return updated
}

func userDexRarityComplete(user string, rarity string) bool {
	kinds := getBassKinds()[rarity]
	if len(kinds) == 0 {
		fmt.Printf("Got invalid rarity: %v \n", rarity)
		return false
	}

	for _, kind := range kinds {
		if !UserDex[user][kind].Caught {
			return false
		}
	}

	return true
}

func dexString(user string) string {
	fmt.Printf("%v \n", UserDex[user])
	dexString := fmt.Sprintf("__%v's BassDex__\n\n", user)

	rarityHeaders := make(map[string]string)
	rarityHeaders["Epic"] = ":purple_circle: EPIC :purple_circle: "
	rarityHeaders["Rare"] = ":green_circle: RARE :green_circle: "
	rarityHeaders["Uncommon"] = ":yellow_circle: UNCOMMON :yellow_circle: "
	rarityHeaders["Common"] = ":white_circle: COMMON :white_circle: "
	rarityOrder := []string{"Epic", "Rare", "Uncommon", "Common"}

	for _, rarity := range rarityOrder {
		if userDexRarityComplete(user, rarity) {
			dexString += ":star:   "
		}
		dexString += rarityHeaders[rarity]
		var rarityEntries []string
		for _, kind := range getBassKinds()[rarity] {
			dexEntry := UserDex[user][kind]
			if dexEntry.Caught {
				rarityEntries = append(rarityEntries, fmt.Sprintf("<[**%v** - *%v* - PR: %vcm]>", kind, dexEntry.FirstCaught.Format(layoutUS), dexEntry.LargestCaught))
			} else {
				rarityEntries = append(rarityEntries, "<[:question:]>")
			}
		}
		dexString += strings.Join(rarityEntries, " ... ") + "\n"
		if rarity != "Common" {
			dexString += "\n"
		}
	}

	return dexString
}

func readLastSportsQuoteDate() (time.Time, error) {
	fmt.Println("loading last sports quote timestamp from file...")
	quoteFile, _ := ioutil.ReadFile("lastquotetime.txt")
	return time.Parse(time.RFC3339, string(quoteFile))
}

func updateLastSportsQuoteDate() {
	t := time.Now()
	err := ioutil.WriteFile("lastquotetime.txt", []byte(t.Format(time.RFC3339)), 0644)
	if err != nil {
		fmt.Printf("Error saving timestamp: %v\n", err)
	}
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

	fmt.Println("loading dexes from file...")
	dexFile, _ := ioutil.ReadFile("dexes.json")

	json.Unmarshal([]byte(dexFile), &UserDex)
	fmt.Println("Dexes load successful. Loaded UserDex:")
	for user, dex := range UserDex {
		fmt.Printf("%v: %v", user, dex)
	}
}

func save() {
	stashFile, _ := json.MarshalIndent(BassMap, "", "    ")
	_ = ioutil.WriteFile("stashes.json", stashFile, 0644)
	baitFile, _ := json.MarshalIndent(UserBait, "", "    ")
	_ = ioutil.WriteFile("bait_charges.json", baitFile, 0644)
	dexFile, _ := json.MarshalIndent(UserDex, "", "    ")
	_ = ioutil.WriteFile("dexes.json", dexFile, 0644)
}

// Bait types: fly fishing, lure - jig, lure - spoon, lure - spinner, lure -crankbait, lure - plug, powerbait - plain, powerbait - glitter, worm, minnow, crayfish, cricket, frog, offal
