package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"net/url"
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

	_ "modernc.org/sqlite"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
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

	oauthConfig = &oauth2.Config{
		ClientID:     GOOGLE_ID,
		ClientSecret: GOOGLE_SECRET,
		RedirectURL:  REDIRECT_URL,
		Scopes:       []string{"https://www.googleapis.com/auth/spreadsheets"},
		Endpoint:     google.Endpoint,
	}
)

func runMigrations(db *sql.DB) {
	driver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		log.Fatal("migrate driver:", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://./db/migrations",
		"sqlite", driver)
	if err != nil {
		log.Fatal("migrate init:", err)
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatal("migrate up:", err)
	}

}

func initDB() *sql.DB {
	db, err := sql.Open("sqlite", "data.db")
	if err != nil {
		log.Fatal(err)
	}

	runMigrations(db)
	return db
}

func getUserGSheet(db *sql.DB, userID int64) (refreshToken string, gsheetUrl string, gsheetName string, err error) {
	row := db.QueryRow(`
		SELECT refresh_token, google_sheet_url, google_sheet_name
		FROM user_gsheet
		WHERE user_id = ?
		`, userID)
	err = row.Scan(&refreshToken, &gsheetUrl, &gsheetName)
	if err != nil {
		return "", "", "", err
	}

	return refreshToken, gsheetUrl, gsheetName, nil
}

func initBot(db *sql.DB) (*tele.Bot, error) {
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
/token {callback_url} - set google token after auth
/status - Auth status
/use_sheet {sheet_url} {sheet_name: eg, Sheet1!A:D} - connect google sheet`

		return c.Send(startMessage)
	})

	b.Handle("/auth", func(c tele.Context) error {
		userID := c.Sender().ID
		state := strconv.FormatInt(userID, 10)
		url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
		return c.Send("Connect Google Sheets:\n" + url)
	})

	b.Handle("/status", func(c tele.Context) error {
		userID := c.Sender().ID

		var (
			refreshToken string
			gsheetUrl    string
			gsheetName   string
		)
		row := db.QueryRow(`
		SELECT refresh_token, google_sheet_url, google_sheet_name
		FROM user_gsheet
		WHERE user_id = ?
		`, userID)
		row.Scan(&refreshToken, &gsheetUrl, &gsheetName)

		missing := []string{}
		if refreshToken == "" {
			missing = append(missing, "token")
		}
		if gsheetUrl == "" {
			missing = append(missing, "sheet")
		}

		if len(missing) == 0 {
			return c.Send("all good")
		}

		missingMsg := strings.Join(missing, ", ")

		return c.Send(fmt.Sprintf("%s not connected. Use /auth and /use_sheets", missingMsg))
	})

	b.Handle("/use_sheet", func(c tele.Context) error {
		sheetUrl := c.Args()[0]
		sheet := c.Args()[1]
		// todo: validate

		userID := c.Sender().ID

		_, err := db.Exec(`
			UPDATE user_gsheet SET
				google_sheet_url = ?,
				google_sheet_name = ?
			WHERE user_id = ?
		`, sheetUrl, sheet, userID)
		if err != nil {
			log.Println("save google sheet error:", err)
			return c.Send("save google sheet error")
		}

		return c.Send("saved")
	})

	b.Handle("/token", func(c tele.Context) error {
		callbackUrl := c.Args()[0]
		if callbackUrl == "" {
			return c.Send("url is empty")
		}

		u, err := url.Parse(callbackUrl)
		if err != nil {
			log.Println("url.Parse err:", err)
			return c.Send("url format error")
		}

		q := u.Query()
		code := q.Get("code")

		token, err := oauthConfig.Exchange(context.Background(), code)
		if err != nil {
			log.Println("error oauth exchange:", err)
			return c.Send("oauth exchange error")
		}

		userID := c.Sender().ID

		_, err = db.Exec(`
			INSERT INTO user_gsheet(user_id, refresh_token) VALUES (?, ?)
			ON CONFLICT(user_id) DO UPDATE SET refresh_token = excluded.refresh_token
		`,
			userID, token.RefreshToken,
		)
		if err != nil {
			log.Println("store token error:", err)
			return c.Send("store token error")
		}

		return c.Send("Google sheets connected")
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

		refreshToken, gsheetUrl, gsheetName, err := getUserGSheet(db, userID)
		if err != nil {
			log.Println("getUserGSheet error:", err)
		}

		if refreshToken == "" {
			return c.Send("not authenticated")
		}

		userToken := &oauth2.Token{RefreshToken: refreshToken}
		sheetService, err := sheets.NewService(ctx, option.WithTokenSource(oauthConfig.TokenSource(ctx, userToken)))
		if err != nil {
			fmt.Println("error initializing sheet service")
			return c.Send("internal server error")
		}

		valueRange := &sheets.ValueRange{
			Values: [][]interface{}{
				func() []interface{} {
					row := make([]interface{}, len(trimmedInput)+1)
					row[0] = time.Now().Format(time.DateOnly)
					for i, v := range trimmedInput {
						row[i+1] = v
					}
					return row
				}(),
			},
		}

		re := regexp.MustCompile(`/spreadsheets/d/([a-zA-Z0-9-_]+)`)
		matches := re.FindStringSubmatch(gsheetUrl)
		if len(matches) < 2 {
			fmt.Println("error extract sheet id:", gsheetUrl)
			return c.Send("unable to extract sheet id")
		}
		spreadSheetId := matches[1]

		_, err = sheetService.Spreadsheets.Values.Append(spreadSheetId, fmt.Sprintf("%s", gsheetName), valueRange).
			ValueInputOption("USER_ENTERED").
			Do()
		if err != nil {
			fmt.Println("failed to insert row")
			return c.Send("failed to insert row")
		}

		return c.Send("Row inserted!")
	})

	return b, nil
}

// func startCallbackServer(bot *tele.Bot) {
// 	http.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
// 		state := r.URL.Query().Get("state")
// 		code := r.URL.Query().Get("code")
//
// 		userID, err := strconv.ParseInt(state, 10, 64)
// 		if err != nil {
// 			log.Println("unparseable userID:", err)
// 			http.Error(w, "bad request", http.StatusBadRequest)
// 			return
// 		}
//
// 		token, err := oauthConfig.Exchange(r.Context(), code)
// 		if err != nil {
// 			log.Println("error oauth exchange:", err)
// 			http.Error(w, "internal server error", http.StatusInternalServerError)
// 			return
// 		}
//
// 		tokenStore[userID] = token
//
// 		bot.Send(&tele.Chat{ID: userID}, "Google sheets connected")
// 		fmt.Fprintln(w, "Auth successful. Can close this tab.")
// 	})
//
// 	go http.ListenAndServe(":8080", nil)
// }

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
	db := initDB()

	bot, err := initBot(db)
	if err != nil {
		log.Fatal("error initBot():", err)
		return
	}

	// startCallbackServer(bot)

	bot.Start()
}
