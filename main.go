package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Cyclenerd/scrumpoker/store"
	"github.com/google/uuid"
)

const (
	// templatesDir is the directory where HTML templates are stored.
	templatesDir = "./templates"

	// staticDir is the directory where static assets (CSS, JS, images) are stored.
	staticDir = "./static"

	// cookiePrefix is the prefix used for the session cookie name.
	cookiePrefix = "scrumpoker_session_"

	// streamMaxDuration is the maximum duration for an SSE stream connection.
	// 4m30s is chosen to stay just under the typical 5m proxy timeout (e.g. Google Cloud Run).
	streamMaxDuration = 4*time.Minute + 30*time.Second

	// maxFormBytes is the maximum size allowed for POST form data.
	maxFormBytes = 4 * 1024

	// maxNameLength is the maximum number of characters allowed for a player's name.
	maxNameLength = 16
)

var (
	// templates holds the parsed HTML templates used for rendering pages.
	templates *template.Template

	// storeEngine is the backend storage implementation for rooms and player data.
	storeEngine *store.StoreEngine
)

func main() {
	var err error

	// Parse and cache all HTML templates at startup.
	// This helps catch syntax errors early and improves performance.
	templates, err = template.New("").Funcs(template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}).ParseGlob(filepath.Join(templatesDir, "*.html"))
	if err != nil {
		log.Fatal("Failed to load templates:", err)
	}

	// Initialize the in-memory store for rooms.
	// We check for an environment variable to potentially persist data.
	storageDir := os.Getenv(store.StorageDirEnvVar)
	storeEngine, err = store.NewStoreEngine(storageDir)
	if err != nil {
		log.Fatal("Failed to initialize store:", err)
	}

	// Start a background goroutine to handle graceful shutdown (e.g., save state on exit).
	watchForShutdown()

	// Serve static assets (CSS, images, etc.) directly.
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticDir, "img", "icon", "favicon.ico"))
	})
	http.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticDir, "robots.txt"))
	})

	// Register application routes.
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/create", handleCreate)
	http.HandleFunc("/room/", handleRoom)
	http.HandleFunc("/join/", handleJoin)
	http.HandleFunc("/vote/", handleVote)
	http.HandleFunc("/reveal/", handleReveal)
	http.HandleFunc("/reset/", handleReset)
	http.HandleFunc("/stream/", handleStream)
	http.HandleFunc("/logout/", handleLogout)

	addr := ":8080"
	server := &http.Server{
		// Addr specifies the TCP address for the server to listen on, in the form "host:port".
		Addr: addr,

		// ReadTimeout is the maximum duration for reading the entire request, including the body.
		ReadTimeout: 15 * time.Second,

		// ReadHeaderTimeout is the amount of time allowed to read request headers.
		// This is important to prevent Slowloris attacks where a client sends headers very slowly.
		ReadHeaderTimeout: 10 * time.Second,

		// WriteTimeout is the maximum duration before timing out writes of the response.
		// It is set to the stream duration plus a buffer to ensure SSE connections aren't closed prematurely.
		WriteTimeout: streamMaxDuration + time.Minute,

		// IdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled.
		IdleTimeout: 2 * time.Minute,

		// MaxHeaderBytes controls the maximum number of bytes the server will read parsing the request header's keys and values.
		// 1 << 20 is 1 MB (1048576 bytes).
		MaxHeaderBytes: 1 << 20,
	}

	log.Printf("Scrum Poker Server starting on http://localhost%s", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("Server failed:", err)
	}
}

// handleIndex serves the landing page.
// Since it's registered at "/", it acts as a catch-all, so we must manually check for 404s.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	renderTemplate(w, "index", nil)
}

// handleCreate creates a new Scrum Poker room.
// It handles both the initial page load (GET) and the form submission (POST).
func handleCreate(w http.ResponseWriter, r *http.Request) {
	// If not POST, just render the room creation form.
	if r.Method != http.MethodPost {
		renderTemplate(w, "create", nil)
		return
	}

	// Parse the form data with size limits to prevent abuse.
	if !parseFormWithLimit(w, r) {
		return
	}

	// Generate unique IDs for the Room and the Session (Game Master).
	roomID := generateUUID()
	sessionID := generateUUID()

	// Validate the user input.
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Game Master"
	}
	if len(name) > maxNameLength {
		http.Error(w, "Name is too long", http.StatusBadRequest)
		return
	}

	// Create the room in the in-memory store.
	if err := storeEngine.CreateRoom(roomID, sessionID, name); err != nil {
		log.Println("Failed to create room:", err)
		http.Error(w, "Unable to create room", http.StatusInternalServerError)
		return
	}

	// Set the session cookie so the user is authenticated as the Game Master.
	setSessionCookie(w, r, roomID, sessionID)

	// Redirect the user to the newly created room.
	http.Redirect(w, r, "/room/"+roomID, http.StatusSeeOther)
}

// handleJoin adds a player to an existing room.
// It expects a POST request with the player's name.
func handleJoin(w http.ResponseWriter, r *http.Request) {
	// Ensure it's a POST request (the form submission).
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract the Room ID from the URL.
	roomID, err := roomIDFromPath(r.URL.Path, "/join/")
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	// Parse and validate the form data.
	if !parseFormWithLimit(w, r) {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if len(name) > maxNameLength {
		http.Error(w, "Name is too long", http.StatusBadRequest)
		return
	}

	// Generate a new session ID and try to join the room.
	sessionID := generateUUID()
	if err := storeEngine.JoinRoom(roomID, sessionID, name); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Println("Failed to join room:", err)
		http.Error(w, "Unable to join room", http.StatusInternalServerError)
		return
	}

	// Set the session cookie and redirect to the room page.
	setSessionCookie(w, r, roomID, sessionID)
	http.Redirect(w, r, "/room/"+roomID, http.StatusSeeOther)
}

// handleRoom renders the main poker UI, ensuring the viewer is part of the room and only exposing safe data.
func handleRoom(w http.ResponseWriter, r *http.Request) {
	// Identify the room from the URL.
	roomID, err := roomIDFromPath(r.URL.Path, "/room/")
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		renderTemplate(w, "error", map[string]interface{}{"Error": "Room not found"})
		return
	}

	// Check if the user has a valid session cookie for this room.
	cookie, err := r.Cookie(cookiePrefix + roomID)
	if err != nil || cookie.Value == "" {
		// If not, show the join screen.
		renderTemplate(w, "join", map[string]interface{}{"RoomID": roomID})
		return
	}

	// Fetch the current room state.
	room, err := storeEngine.RoomSnapshot(roomID)
	if err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			w.WriteHeader(http.StatusNotFound)
			renderTemplate(w, "error", map[string]interface{}{"Error": "Room not found"})
			return
		}
		log.Println("Failed to fetch room state:", err)
		http.Error(w, "Unable to load room", http.StatusInternalServerError)
		return
	}

	// Sanitize the room data (hide other players' votes if not revealed).
	room = room.SanitizedCopy(cookie.Value, false)

	// Get the player's own details.
	player, err := storeEngine.Player(roomID, cookie.Value)
	if err != nil {
		// If the player isn't found (e.g., server restart), ask them to join again.
		renderTemplate(w, "join", map[string]interface{}{"RoomID": roomID})
		return
	}

	isGameMaster := room.GameMaster == cookie.Value
	maskedSessionID := store.MaskSessionID(cookie.Value)

	// Render the room template with the sanitized state.
	renderTemplate(w, "room", map[string]interface{}{"RoomID": roomID, "Room": room, "SessionID": maskedSessionID, "Player": player, "IsGameMaster": isGameMaster})
}

// handleVote stores the player's selection and keeps their session active.
func handleVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify Authentication.
	roomID, err := roomIDFromPath(r.URL.Path, "/vote/")
	if err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	cookie, err := r.Cookie(cookiePrefix + roomID)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse the vote value.
	if !parseFormWithLimit(w, r) {
		return
	}

	value := strings.TrimSpace(r.FormValue("value"))
	if value == "" {
		http.Error(w, "Vote value required", http.StatusBadRequest)
		return
	}

	// Validate the vote options.
	switch value {
	case "0", "1", "2", "3", "5", "8", "13", "21", "?", "☕":
		// Valid value
	default:
		http.Error(w, "Invalid vote value", http.StatusBadRequest)
		return
	}

	// Update the room state.
	if err := storeEngine.RegisterVote(roomID, cookie.Value, value); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, store.ErrPlayerNotFound) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		log.Println("Failed to register vote:", err)
		http.Error(w, "Unable to store vote", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleReveal flips every card but only when the Scrum Master approves.
func handleReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify Authentication.
	roomID, err := roomIDFromPath(r.URL.Path, "/reveal/")
	if err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	cookie, err := r.Cookie(cookiePrefix + roomID)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Ensure the player is still part of the room.
	if _, err := storeEngine.Player(roomID, cookie.Value); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Verify that the requester is the Game Master.
	isGM, err := storeEngine.IsGameMaster(roomID, cookie.Value)
	if err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		log.Println("Failed to verify permissions:", err)
		http.Error(w, "Unable to reveal votes", http.StatusInternalServerError)
		return
	}
	if !isGM {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Reveal the votes for everyone.
	if err := storeEngine.SetReveal(roomID, true); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		log.Println("Failed to reveal votes:", err)
		http.Error(w, "Unable to reveal votes", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleReset starts a fresh round by clearing votes, again restricted to the Scrum Master.
func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify Authentication.
	roomID, err := roomIDFromPath(r.URL.Path, "/reset/")
	if err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	cookie, err := r.Cookie(cookiePrefix + roomID)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Ensure the player is still part of the room.
	if _, err := storeEngine.Player(roomID, cookie.Value); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Verify that the requester is the Game Master.
	isGM, err := storeEngine.IsGameMaster(roomID, cookie.Value)
	if err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		log.Println("Failed to verify permissions:", err)
		http.Error(w, "Unable to reset room", http.StatusInternalServerError)
		return
	}
	if !isGM {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Reset the room state (clear votes, hide cards).
	if err := storeEngine.ResetVotes(roomID); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		log.Println("Failed to reset votes:", err)
		http.Error(w, "Unable to reset room", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleStream pushes sanitized room snapshots over SSE, acting as our live update channel.
func handleStream(w http.ResponseWriter, r *http.Request) {
	// Authenticate and Validate.
	roomID, err := roomIDFromPath(r.URL.Path, "/stream/")
	if err != nil {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}
	cookie, err := r.Cookie(cookiePrefix + roomID)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if _, err := storeEngine.Player(roomID, cookie.Value); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Set headers for Server-Sent Events (SSE).
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Helper to capture the current state and send it as a JSON payload.
	sendSnapshot := func() error {
		room, err := storeEngine.RoomSnapshot(roomID)
		if err != nil {
			return err
		}

		// Important: Sanitize data so players don't see hidden votes.
		room = room.SanitizedCopy(cookie.Value, true)

		payload, err := json.Marshal(room)
		if err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// Send the initial state immediately.
	if err := sendSnapshot(); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		log.Println("Failed to send initial stream payload:", err)
		return
	}

	// Start the tick loop to push updates every second.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// Close stream connections before the Google Cloud Run timeout does
	// letting clients reconnect cleanly and eliminating the "Truncated response body" warning.
	timeout := time.NewTimer(streamMaxDuration)
	defer timeout.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
			if err := sendSnapshot(); err != nil {
				if !errors.Is(err, store.ErrRoomNotFound) {
					log.Println("Failed to send stream update:", err)
				}
				return
			}
		}
	}
}

// handleLogout logs a player out of the room.
// It removes the player from the store and clears the session cookie.
func handleLogout(w http.ResponseWriter, r *http.Request) {
	roomID, err := roomIDFromPath(r.URL.Path, "/logout/")
	if err != nil {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	// Try to remove player from the room store if they have a valid session
	if cookie, err := r.Cookie(cookiePrefix + roomID); err == nil && cookie.Value != "" {
		if err := storeEngine.RemovePlayer(roomID, cookie.Value); err != nil && !errors.Is(err, store.ErrRoomNotFound) && !errors.Is(err, store.ErrPlayerNotFound) {
			log.Println("Failed to remove player from room:", err)
		}
	}

	// Invalidate the session cookie by setting MaxAge to -1
	http.SetCookie(w, &http.Cookie{
		Name:     cookiePrefix + roomID,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	renderTemplate(w, "logout", map[string]interface{}{"RoomID": roomID})

}

// watchForShutdown intercepts SIGINT/SIGTERM to flush in-memory rooms for durability.
// https://docs.cloud.google.com/run/docs/container-contract#instance-shutdown
func watchForShutdown() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		log.Printf("Received %s signal, flushing rooms to disk", sig)
		if storeEngine != nil {
			if err := storeEngine.FlushAll(); err != nil {
				log.Printf("Failed to flush store: %v", err)
			}
		}
		os.Exit(0)
	}()
}

// generateUUID generates a new random UUID string.
func generateUUID() string {
	return uuid.NewString()
}

// setSessionCookie sets an HttpOnly, Secure cookie for the session.
// This cookie is scoped to the specific room and expires in 2 days.
func setSessionCookie(w http.ResponseWriter, r *http.Request, roomID, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:  cookiePrefix + roomID,
		Value: sessionID,
		Path:  "/",

		// HttpOnly prevents JavaScript access to the cookie, mitigating XSS risks.
		HttpOnly: true,

		// Secure ensures the cookie is only sent over HTTPS.
		Secure: isSecureRequest(r),

		// SameSite=StrictMode prevents CSRF attacks.
		SameSite: http.SameSiteStrictMode,

		// MaxAge is set to 2 days (in seconds).
		MaxAge: 86400 * 2,
	})
}

// parseFormWithLimit parses the request form but limits the body size to prevent DoS attacks.
// It returns true if parsing was successful, false otherwise.
func parseFormWithLimit(w http.ResponseWriter, r *http.Request) bool {
	// r.Body is wrapped with MaxBytesReader to enforce the size limit.
	// If the body exceeds maxFormBytes, Read() will return an error.
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)

	if err := r.ParseForm(); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			// Specific error for when the body is too large.
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			// Generic error for other parsing issues.
			http.Error(w, "Invalid form submission", http.StatusBadRequest)
		}
		return false
	}
	return true
}

// roomIDFromPath extracts and validates the Room ID from the URL path.
// It ensures that the path starts with the expected prefix and that the ID is a valid UUID.
func roomIDFromPath(path, prefix string) (string, error) {
	if !strings.HasPrefix(path, prefix) {
		return "", errors.New("invalid room path")
	}

	// Remove the prefix and any surrounding slashes to isolate the ID.
	roomID := strings.Trim(strings.TrimPrefix(path, prefix), "/")

	// Basic validation: emptiness and slash check.
	if roomID == "" || strings.Contains(roomID, "/") {
		return "", errors.New("invalid room ID")
	}

	// Strong validation: check if it's a valid UUID format.
	if _, err := uuid.Parse(roomID); err != nil {
		return "", errors.New("invalid room ID")
	}

	return roomID, nil
}

// isSecureRequest determines if the incoming request is using HTTPS.
// It checks the TLS connection state and standard proxy headers.
func isSecureRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	// Direct TLS connection
	if r.TLS != nil {
		return true
	}

	// X-Forwarded-Proto header (standard for most proxies like from AWS, Google or Nginx)
	// https://docs.cloud.google.com/functions/docs/reference/headers#x-forwarded-proto
	if proto := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return true
	}

	// X-Forwarded-Scheme header (alternative used by some other proxies)
	if scheme := r.Header.Get("X-Forwarded-Scheme"); strings.EqualFold(scheme, "https") {
		return true
	}

	return false
}

// renderTemplate executes the specified HTML template with the provided data.
// It handles any errors during execution by logging them and sending a 500 Internal Server Error to the client.
func renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	err := templates.ExecuteTemplate(w, name+".html", data)
	if err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		log.Println("Template execution error:", err)
	}
}
