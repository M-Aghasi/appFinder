// AppFinderBot project main.go
package main

import (
	"github.com/M-Aghasi/appFinder/searchApi"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"time"
	"os"

	"github.com/go-redis/redis"
	"gopkg.in/telegram-bot-api.v4"
)

const REDIS_CACHE_EXPIRE_SECS int = 60 * 60 * 3

var redisClient *redis.Client = nil
var botToken string
var logFile string
var redisHost string
var redisPassword string
var ignoreRedisPassword string

// Entry point of server, initializes BotAPI, gets updates channel and listens for updates
func main() {
	if len(os.Args) != 6 {
		log.Panic("This service requires 4 args: 1-botToken, 2-logFile, 3-redisHost, 4-redisPassword 5-ignoreRedisPassword")
	}
	botToken = os.Args[1]
	logFile = os.Args[2]
	redisHost = os.Args[3]
	redisPassword = os.Args[4]
	ignoreRedisPassword = os.Args[5]
	log.Println("Args %s %s %s %s %s", botToken, logFile, redisHost, len(redisPassword), ignoreRedisPassword)

	f, err := os.OpenFile(logFile, os.O_RDWR | os.O_CREATE | os.O_APPEND, 0666)
	if err != nil {
	    log.Panic("Opening log file failed, err: ", err.Error())
	}
	defer f.Close()
	log.SetOutput(f)

	initRedisClient()
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic("AppFinder bot creation failed, err: " + err.Error())
	}
	log.Println("Bot Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 40
	updates, err := bot.GetUpdatesChan(u)
	if err != nil {
		log.Panic("Getting updates channel failed, err: " + err.Error())
	}

	for update := range updates {
		if update.Message == nil || update.Message.Text == "" {
			continue
		}
		log.Printf("text message received from [%s]: %s", update.Message.From.UserName, update.Message.Text)
		dispatchUpdate(bot, update)
	}
}

// Dispatches arriving updates to appropriate handler
//
// bot is BotAPI, update is arrived telegram update
func dispatchUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message.IsCommand() {
		if update.Message.Command() == "start" {
			handleStart(bot, update)
			return
		}
		if update.Message.Command() == "help" {
			handleHelp(bot, update)
			return
		}
		log.Printf("Unsupported command received: " + update.Message.Command())
		return
	}

	if strings.HasPrefix(update.Message.Text, "(ID: ") && strings.Contains(update.Message.Text, ") - ") {
		startIdx := len("(ID: ")
		endIdx := strings.LastIndex(update.Message.Text, ") - ")
		runes := []rune(update.Message.Text)
		id := string(runes[startIdx:endIdx])
		handleSpecificId(id, bot, update)
		return
	}
	handleSearch(update.Message.Text, bot, update)
}

// handler for telegram /start command
//
// bot is BotAPI, update is arrived telegram update
func handleStart(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	htmlStr := "With AppFinder you can search AppStore's apps and games by title!\n"
	htmlStr += "For starting a search you must send a search query as a regular message."
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, htmlStr)
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

// handler for telegram /help command
//
// bot is BotAPI, update is arrived telegram update
func handleHelp(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	htmlStr := "With AppFinder you can search AppStore's apps and games by title!\n"
	htmlStr += "For starting a search you must send a search query as a regular message."
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, htmlStr)
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

// handler for search queries
//
// query is the received search term from user, bot is BotAPI, update is arrived telegram update
func handleSearch(query string, bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	resChan := make(chan []searchApi.AppleSearchAppInfo)
	go searchApi.SearchApp(resChan, query)
	results := <-resChan
	if len(results) < 1 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "No result found.")
		bot.Send(msg)
		return
	}

	var rows = make([][]tgbotapi.KeyboardButton, len(results))
	for i := 0; i < len(results); i++ {
		rows[i] = tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("(ID: " + strconv.FormatInt(results[i].IosId, 10) + ") - " + results[i].Name),
		)
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Choose one of Results:")
	replayKeyboardMarkup := tgbotapi.NewReplyKeyboard(rows...)
	replayKeyboardMarkup.OneTimeKeyboard = true
	msg.ReplyMarkup = replayKeyboardMarkup
	bot.Send(msg)

	saveResultsToCache(results)
}

// handler for getting an specific app's details
//
// id is target app's id to seek details for, bot is BotAPI, update is arrived telegram update
func handleSpecificId(id string, bot *tgbotapi.BotAPI, update tgbotapi.Update) {

	if handleSpecificIdByCache(id, bot, update) {
		return
	}

	resChan := make(chan []searchApi.AppleSearchAppInfo)
	go searchApi.LookupApp(resChan, id)
	results := <-resChan
	if results == nil || len(results) < 1 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "No result found.")
		msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
		bot.Send(msg)
		return
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, getHtmlOfAppInfo(results[0]))
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
	bot.Send(msg)

	saveResultsToCache(results)
}

// handler for getting an specific app's details from cache
//
// id is target app's id to seek details for, bot is BotAPI, update is arrived telegram update
// returns true on success, false on failure
func handleSpecificIdByCache(id string, bot *tgbotapi.BotAPI, update tgbotapi.Update) bool {
	val, err := redisClient.Get("app_info:" + id).Result()
	if err == nil {
		appInfo := searchApi.AppleSearchAppInfo{}
		err2 := json.Unmarshal([]byte(val), &appInfo)
		if err2 == nil {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, getHtmlOfAppInfo(appInfo))
			msg.ParseMode = "HTML"
			msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
			bot.Send(msg)

			err3 := redisClient.Expire("app_info:"+id, time.Duration(REDIS_CACHE_EXPIRE_SECS)*time.Second).Err()
			if err3 != nil {
				log.Println("Error in extending cache key expiration, err: " + err3.Error())
			}
			return true
		}
		log.Println("Parsing Redis response as json failed with error: " + err2.Error())
	} else if err != redis.Nil {
		log.Println("Getting Redis row failed with error: " + err.Error())
	}

	return false
}

// Return html representation for an AppleSearchAppInfo object
//
// appInfo is the AppleSearchAppInfo object which we want HTML representation for
func getHtmlOfAppInfo(appInfo searchApi.AppleSearchAppInfo) string {
	htmlStr := "<b>" + appInfo.Name + "</b>\n"
	htmlStr += "<b>Ios ID:</b> " + strconv.FormatInt(appInfo.IosId, 10) + "\n"
	htmlStr += "<b>Bundle name:</b> " + appInfo.IosBundleName + "\n"
	htmlStr += "<b>Category:</b> " + appInfo.Category + "\n"
	htmlStr += "<b>Publisher:</b> " + appInfo.Publisher + "\n"
	htmlStr += "<b>Release date:</b> " + appInfo.ReleaseDate + "\n"
	htmlStr += "<b>File size:</b> " + appInfo.IosFileSize + "\n"
	htmlStr += "<b>Min ios version:</b> " + appInfo.IosMinOs + "\n"
	htmlStr += "<b>Content rating:</b> " + appInfo.ContentRating + "\n"
	htmlStr += "<b>Price:</b> " + fmt.Sprintf("%.2f", appInfo.IosPrice) + "\n"
	htmlStr += "<b>Score:</b> " + fmt.Sprintf("%.1f", appInfo.IosScore) + "\n"
	htmlStr += "<b>Store url:</b> " + appInfo.IosUrl + "\n\n-----------------------------\n\n"
	htmlStr += "<b>Description:</b> " + html.EscapeString(appInfo.Desc) + "\n"
	return htmlStr
}

// Initializes RedisClient for caching functionalities
func initRedisClient() {
	if ignoreRedisPassword == "yes" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisHost,
			DB:       0,
		})
		return
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr:     redisHost,
		Password: redisPassword,
		DB:       0,
	})

	pong, err := redisClient.Ping().Result()
	fmt.Println(pong, err)
	if err != nil {
		log.Panic("Error on initializing Redis client, err: " + err.Error())
	}
}

// This method saves all received AppleSearchAppInfo objs to redis for caching
func saveResultsToCache(results []searchApi.AppleSearchAppInfo) {
	for i := 0; i < len(results); i++ {

		val, err := json.Marshal(results[i])
		if err != nil {
			log.Println("Error in Marshal appInfo for saving to redis, err" + err.Error())
			continue
		}

		err = redisClient.Set("app_info:"+strconv.FormatInt(results[i].IosId, 10),
			string(val),
			time.Duration(REDIS_CACHE_EXPIRE_SECS)*time.Second).Err()
		if err != nil {
			log.Println("Error in Set key to Redis, err" + err.Error())
			continue
		}
	}
}
