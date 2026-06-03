package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"google.golang.org/api/option"
	sheets "google.golang.org/api/sheets/v4"
)

type SheetStore struct {
	SheetUrl string
	Sheet    string
}

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

	sheetStore = map[int64]SheetStore{}

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
		startMessage := `/auth - Authenticate Google Sheets
/status - Auth status
/use_sheet {sheet_url} {sheet_name} - connect google sheet`

		return c.Send(startMessage)
	})

	b.Handle("/auth", func(c tele.Context) error {
		userID := c.Sender().ID
		state := strconv.FormatInt(userID, 10)
		url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
		return c.Send("Connect Google Sheets:\n" + url)
	})

	b.Handle("/status", func(c tele.Context) error {
		userID := c.Sender().ID
		_, tokenOk := tokenStore[userID]
		_, sheetOk := sheetStore[userID]
		missing := []string{}
		if !tokenOk {
			missing = append(missing, "token")
		}
		if !sheetOk {
			missing = append(missing, "sheet")
		}

		missingMsg := strings.Join(missing, ", ")

		return c.Send(fmt.Sprintf("%s not connected. Use /auth and /use_sheets", missingMsg))
	})

	b.Handle("/use_sheet", func(c tele.Context) error {
		sheetUrl := c.Args()[0]
		sheet := c.Args()[1]
		// todo: validate

		userID := c.Sender().ID
		sheetStore[userID] = SheetStore{
			SheetUrl: sheetUrl,
			Sheet:    sheet,
		}

		return c.Send("saved")
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		userID := c.Sender().ID
		userText := c.Text()
		splittedText := strings.Split(userText, ",")

		var trimmedInput []string
		for _, v := range splittedText {
			trimmedInput = append(trimmedInput, strings.TrimSpace(v))
		}

		ctx := context.Background()
		userToken, isTokenExists := tokenStore[userID]
		if !isTokenExists {
			return c.Send("not authenticated")
		}

		sheetService, err := sheets.NewService(ctx, option.WithTokenSource(oauthConfig.TokenSource(ctx, userToken)))
		if err != nil {
			fmt.Println("error initializing sheet service")
			return c.Send("internal server error")
		}

		valueRange := &sheets.ValueRange{
			Values: [][]interface{}{
				func() []interface{} {
					row := make([]interface{}, len(trimmedInput))
					for i, v := range trimmedInput {
						row[i] = v
					}
					return row
				}(),
			},
		}

		userSheet := sheetStore[userID]
		re := regexp.MustCompile(`/spreadsheets/d/([a-zA-Z0-9-_]+)`)
		matches := re.FindStringSubmatch(userSheet.SheetUrl)
		if len(matches) < 2 {
			fmt.Println("error extract sheet id:", userSheet)
			return c.Send("unable to extract sheet id")
		}
		spreadSheetId := matches[1]

		_, err = sheetService.Spreadsheets.Values.Append(spreadSheetId, fmt.Sprintf("%s!A1", userSheet.Sheet), valueRange).
			ValueInputOption("USER_ENTERED").
			InsertDataOption("INSERT_ROWS").
			Do()
		if err != nil {
			fmt.Println("failed to insert row")
			return c.Send("failed to insert row")
		}

		return c.Send("Row inserted!")
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
