CREATE TABLE user_gsheet (
  user_id INTEGER PRIMARY KEY,
  refresh_token TEXT NOT NULL,
  google_sheet_url TEXT NULL,
  google_sheet_name TEXT NULL
)
