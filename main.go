package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	tele "gopkg.in/telebot.v4"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	TG_TOKEN      = os.Getenv("TELEGRAM_BOT_TOKEN")
	GOOGLE_ID     = os.Getenv("GOOGLE_CLIENT_ID")
	GOOGLE_SECRET = os.Getenv("GOOGLE_CLIENT_SECRET")
	REDIRECT_URL  = os.Getenv("GOOGLE_REDIRECT_URL") // default: http://localhost:8080/auth/callback

	// stateStore = map[string]struct {
	// 	userID int64
	// 	expiry time.Time
	// }{}

	tokenStore = map[int64]*oauth2.Token{}

	oauthConfig = &oauth2.Config{
		ClientID:     GOOGLE_ID,
		ClientSecret: GOOGLE_SECRET,
		RedirectURL:  REDIRECT_URL,
		Scopes:       []string{"https://www.googleapis.com/auth/spreadsheets"},
		Endpoint:     google.Endpoint,
	}
)

func initBot() (*tele.Bot, error) {
	pref := tele.Settings{
		Token:  TG_TOKEN,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	b.Handle("/start", func(c tele.Context) error {
		return c.Send("/auth - Authenticate Google Sheets")
	})

	b.Handle("/auth", func(c tele.Context) error {
		userID := c.Sender().ID
		state := strconv.FormatInt(userID, 10)
		url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
		return c.Send("Connect Google Sheets:\n" + url)
	})

	b.Handle("/status", func (c tele.Context) error {
		_, ok := tokenStore[c.Sender().ID]
		if ok {
			return c.Send("connected")
		}
		return c.Send("Not connected. Use /auth")
	})

	return b, nil
}

func startCallbackServer(bot *tele.Bot) {
	http.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")

		userID, err := strconv.ParseInt(state, 10, 64)
		if err != nil {
			log.Println("unparseable userID:", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		token, err := oauthConfig.Exchange(r.Context(), code)
		if err != nil {
			log.Println("error oauth exchange:", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		tokenStore[userID] = token

		bot.Send(&tele.Chat{ID: userID}, "Google sheets connected")
		fmt.Fprintln(w, "Auth successful. Can close this tab.")
	})

	go http.ListenAndServe(":8080", nil)
}

func validateEnv() {
	missing := []string{}
	if TG_TOKEN == "" {
		missing = append(missing, "TELEGRAM_BOT_TOKEN")
	}
	if GOOGLE_ID == "" {
		missing = append(missing, "GOOGLE_CLIENT_ID")
	}
	if GOOGLE_SECRET == "" {
		missing = append(missing, "GOOGLE_CLIENT_SECRET")
	}
	if REDIRECT_URL == "" {
		missing = append(missing, "GOOGLE_REDIRECT_URL")
	}
	if len(missing) > 0 {
		log.Fatalf("missing required env vars: %v", missing)
	}
}

func main() {
	validateEnv()

	bot, err := initBot()
	if err != nil {
		log.Fatal("error initBot():", err)
		return
	}

	startCallbackServer(bot)

	bot.Start()
}
