package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/tunedmystic/authsolo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Date and time formats for Note values.
const (
	NoteDateFormat        = "Jan _2, 2006 3:04 PM"
	NotePartialDateFormat = "January _2, 2006"
	NotePartialTimeFormat = "3:04 PM"
)

// MaxBodyLength is the max amount of characters the Note Body can have.
const MaxBodyLength = 500

//
// ------------------------------------------------------------------
// Models
// ------------------------------------------------------------------
//

// Note is the model for the `notes` table.
type Note struct {
	gorm.Model
	Body string
	Date time.Time

	Tags []Tag `gorm:"many2many:note_tag"`
}

// DisplayDate formats the date as a string.
func (n *Note) DisplayDate() string {
	return n.Date.Format(NotePartialDateFormat)
}

// DisplayTime formats the date's time as a string.
func (n *Note) DisplayTime() string {
	return n.Date.Format(NotePartialTimeFormat)
}

// Tag is the model for the `tags` table.
type Tag struct {
	gorm.Model
	Name string
}

//
// ------------------------------------------------------------------
// Server
// ------------------------------------------------------------------
//

// Server ...
type Server struct {
	Templates     *template.Template
	StaticHandler http.Handler
	DB            *gorm.DB
}

// NewServer ...
func NewServer(db *gorm.DB) Server {
	// TemplatesHTML holds all the html templates.
	//go:embed templates/*
	var TemplatesHTML embed.FS

	// Assets holds all the static assets.
	//go:embed static/*
	var Assets embed.FS

	return Server{
		Templates:     template.Must(template.ParseFS(TemplatesHTML, "templates/*.html")),
		StaticHandler: http.FileServer(http.FS(Assets)),
		DB:            db,
	}
}

// Routes ...
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	// Add authentication middleware to all routes.
	auth := authsolo.New("super-secret")
	r.Use(auth.SoloH)

	r.Get("/", s.HandleIndex)
	r.Get("/static/*", s.HandleStatic)
	r.Get("/note/new", s.HandleNoteCreateForm)             // note create form
	r.Post("/note/new", s.HandleNoteCreate)                // note create action
	r.Get("/note/{noteID}/change", s.HandleNoteUpdateForm) // note update form
	r.Post("/note/{noteID}/change", s.HandleNoteUpdate)    // note update action
	r.Post("/note/{noteID}/delete", s.HandleNoteDelete)    // note delete action

	return auth.Handler(r)
}

// HandleIndex serves the home page.
func (s *Server) HandleIndex(w http.ResponseWriter, r *http.Request) {
	notes := make([]Note, 30)
	s.DB.Preload("Tags").Limit(30).Order("date desc").Find(&notes)
	s.Templates.ExecuteTemplate(w, "index", notes)
}

// HandleStatic serves static assets.
func (s *Server) HandleStatic(w http.ResponseWriter, r *http.Request) {
	s.StaticHandler.ServeHTTP(w, r)
}

// HandleNoteCreateForm serves the Note create form.
func (s *Server) HandleNoteCreateForm(w http.ResponseWriter, r *http.Request) {
	var loc *time.Location
	loc, err := time.LoadLocation("America/New_York")

	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	form := NoteForm{
		Date: now.Format(NotePartialDateFormat),
		Time: now.Format(NotePartialTimeFormat),
	}

	requestContext := NoteFormContext{
		Form:   form,
		URL:    r.URL.Path,
		Action: "create",
	}

	s.Templates.ExecuteTemplate(w, "note-form", requestContext)
}

// HandleNoteCreate performs the Note creation.
func (s *Server) HandleNoteCreate(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, fmt.Sprintf("Something went wrong: %v", err.Error()), http.StatusInternalServerError)
		return
	}

	form := NoteForm{
		Body: r.Form.Get("body"),
		Date: r.Form.Get("date"),
		Time: r.Form.Get("time"),
		Tags: r.Form.Get("tags"),
	}

	if form.IsValid() {
		note := Note{
			Body: form.cleanedBody,
			Date: form.cleanedDateTime,
		}

		// Create Note.
		s.DB.Create(&note)

		// Create Note tags.
		if len(form.cleanedTags) > 0 {
			s.DB.Model(&note).Association("Tags").Append(form.cleanedTags)
		}

		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	requestContext := NoteFormContext{
		Form:   form,
		URL:    r.URL.Path,
		Action: "create",
	}

	s.Templates.ExecuteTemplate(w, "note-form", requestContext)
}

// HandleNoteUpdateForm serves the Note update form.
func (s *Server) HandleNoteUpdateForm(w http.ResponseWriter, r *http.Request) {
	noteID := chi.URLParam(r, "noteID")

	note := Note{}
	if err := s.DB.Preload("Tags").First(&note, noteID).Error; err != nil {
		http.Error(w, fmt.Sprintf("note %v not found", noteID), http.StatusNotFound)
		return
	}

	// Convert slice of tags into a list of comma-separated names.
	tagNames := []string{}
	for _, tag := range note.Tags {
		tagNames = append(tagNames, tag.Name)
	}

	form := NoteForm{
		Body: note.Body,
		Date: note.Date.Format(NotePartialDateFormat),
		Time: note.Date.Format(NotePartialTimeFormat),
		Tags: strings.Join(tagNames, ", "),
	}

	requestContext := NoteFormContext{
		Form:   form,
		URL:    r.URL.Path,
		Action: "update",
		NoteID: note.ID,
	}

	s.Templates.ExecuteTemplate(w, "note-form", requestContext)
}

// HandleNoteUpdate performs the Note update.
func (s *Server) HandleNoteUpdate(w http.ResponseWriter, r *http.Request) {
	noteID := chi.URLParam(r, "noteID")

	note := Note{}
	if err := s.DB.Preload("Tags").First(&note, noteID).Error; err != nil {
		http.Error(w, fmt.Sprintf("note %v not found", noteID), http.StatusNotFound)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, fmt.Sprintf("Something went wrong: %v", err.Error()), http.StatusInternalServerError)
	}

	form := NoteForm{
		Body: r.Form.Get("body"),
		Date: r.Form.Get("date"),
		Time: r.Form.Get("time"),
		Tags: r.Form.Get("tags"),
	}

	if form.IsValid() {
		s.DB.Model(&note).Updates(&Note{Body: form.cleanedBody, Date: form.cleanedDateTime})
		s.DB.Model(&note).Association("Tags").Replace(form.cleanedTags)
		removeStaleTags(s.DB)

		http.Redirect(w, r, "/", http.StatusFound)
	}

	requestContext := NoteFormContext{
		Form:   form,
		URL:    r.URL.Path,
		Action: "update",
		NoteID: note.ID,
	}

	s.Templates.ExecuteTemplate(w, "note-form", requestContext)
}

// HandleNoteDelete performs the Note deletion.
func (s *Server) HandleNoteDelete(w http.ResponseWriter, r *http.Request) {
	noteID := chi.URLParam(r, "noteID")
	s.DB.Unscoped().Delete(&Note{}, noteID)
	removeStaleTags(s.DB)
	http.Redirect(w, r, "/", http.StatusFound)
}

//
// ------------------------------------------------------------------
// Helper structs
// ------------------------------------------------------------------
//

// NoteFormContext provides context data to html templates.
type NoteFormContext struct {
	Form   NoteForm
	URL    string
	Action string
	NoteID uint
}

// NoteForm validates and cleans data for Notes.
type NoteForm struct {
	Date            string
	Time            string
	Body            string
	Tags            string
	Errors          []string
	cleanedDateTime time.Time
	cleanedBody     string
	cleanedTags     []Tag
}

// IsValid checks if the form is valid.
func (form *NoteForm) IsValid() bool {
	form.Validate()
	return len(form.Errors) == 0
}

// Validate performs the form validation.
// Errors are collected and are available via the `.Errors` list.
func (form *NoteForm) Validate() {
	if form.Body == "" {
		form.Errors = append(form.Errors, "Body cannot be blank")
	}

	if len(form.Body) > MaxBodyLength {
		form.Errors = append(form.Errors, "Body is too large")
	}

	form.cleanedBody = strings.Trim(form.Body, " ")

	d, err := time.Parse(NotePartialDateFormat, form.Date)
	if err != nil {
		form.Errors = append(form.Errors, "Invalid Date")
	}

	timeStr := form.Time
	if timeStr == "" {
		timeStr = "12:00 AM"
	}
	t, err := time.Parse(NotePartialTimeFormat, timeStr)
	if err != nil {
		form.Errors = append(form.Errors, "Invalid Time")
	}

	form.cleanedDateTime = time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)

	for _, tagName := range strings.Split(form.Tags, ",") {
		cleanedName := strings.ToLower(strings.Trim(tagName, " "))
		if cleanedName != "" {
			form.cleanedTags = append(form.cleanedTags, Tag{Name: cleanedName})
		}
	}
}

//
// ------------------------------------------------------------------
// Helper functions
// ------------------------------------------------------------------
//

// removeStaleTags deletes Tags that are not linked to Notes.
func removeStaleTags(db *gorm.DB) {
	staleTagIds := []int{}
	db.Raw(`
		select id
		from tags
		where id not in (
			select distinct t.id
			from tags t
			inner join note_tag nt on nt.tag_id = t.id
		);
	`).Scan(&staleTagIds)
	fmt.Printf("Stale Tag ids: %v\n", staleTagIds)
	if len(staleTagIds) > 0 {
		db.Unscoped().Delete(&Tag{}, staleTagIds)
	}
}

//
// ------------------------------------------------------------------
// Entrypoint
// ------------------------------------------------------------------
//

func main() {
	// Init database.
	db, err := gorm.Open(sqlite.Open("simplenotes.sqlite"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		panic(err)
	}

	// Migrate the schema.
	db.AutoMigrate(&Note{}, &Tag{})

	// Init server.
	s := NewServer(db)

	// Start server.
	addr := "localhost:3000"
	fmt.Printf("Running server on %v...\n", addr)
	http.ListenAndServe(addr, s.Routes())
}

/*

	Usage:


	* Run the server:
		> go1.16beta1 run main.go

	* Build the application:
		> go1.16beta1 build -ldflags="-s -w"

	* Run the server and reload on file changes (requires entr):
		> bash -c "find . -type f \( -name '*.go' -o -name '*.html' \) | grep -v 'misc' | entr -r go1.16beta1 run main.go server"

*/
