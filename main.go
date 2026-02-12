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
	templatesDir = "./templates"
	staticDir    = "./static"
	cookiePrefix = "scrumpoker_session_"
)

var (
	templates   *template.Template
	storeEngine *store.StoreEngine
)

func main() {
	var err error

	templates, err = template.New("").Funcs(template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}).ParseGlob(filepath.Join(templatesDir, "*.html"))
	if err != nil {
		log.Fatal("Failed to load templates:", err)
	}

	storageDir := os.Getenv(store.StorageDirEnvVar)
	storeEngine, err = store.NewStoreEngine(storageDir)
	if err != nil {
		log.Fatal("Failed to initialize store:", err)
	}
	watchForShutdown()

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticDir, "img", "icon", "favicon.ico"))
	})
	http.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticDir, "robots.txt"))
	})
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
	log.Printf("Scrum Poker Server starting on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("Server failed:", err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	renderTemplate(w, "index", nil)
}

func handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderTemplate(w, "create", nil)
		return
	}

	roomID := generateUUID()
	sessionID := generateUUID()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Game Master"
	}

	if err := storeEngine.CreateRoom(roomID, sessionID, name); err != nil {
		log.Println("Failed to create room:", err)
		http.Error(w, "Unable to create room", http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, roomID, sessionID)
	http.Redirect(w, r, "/room/"+roomID, http.StatusSeeOther)
}

func handleJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := strings.TrimPrefix(r.URL.Path, "/join/")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

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

	setSessionCookie(w, roomID, sessionID)
	http.Redirect(w, r, "/room/"+roomID, http.StatusSeeOther)
}

// handleRoom renders the main poker UI, ensuring the viewer is part of the room and only exposing safe data.
func handleRoom(w http.ResponseWriter, r *http.Request) {
	roomID := strings.TrimPrefix(r.URL.Path, "/room/")
	if roomID == "" {
		w.WriteHeader(http.StatusNotFound)
		renderTemplate(w, "error", map[string]interface{}{"Error": "Room not found"})
		return
	}

	cookie, err := r.Cookie(cookiePrefix + roomID)
	if err != nil || cookie.Value == "" {
		renderTemplate(w, "join", map[string]interface{}{"RoomID": roomID})
		return
	}

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

	room = room.SanitizedCopy(cookie.Value, false)

	player, err := storeEngine.Player(roomID, cookie.Value)
	if err != nil {
		renderTemplate(w, "join", map[string]interface{}{"RoomID": roomID})
		return
	}

	isGameMaster := room.GameMaster == cookie.Value
	maskedSessionID := store.MaskSessionID(cookie.Value)

	renderTemplate(w, "room", map[string]interface{}{"RoomID": roomID, "Room": room, "SessionID": maskedSessionID, "Player": player, "IsGameMaster": isGameMaster})
}

// handleVote stores the player's selection and keeps their session active.
func handleVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := strings.TrimPrefix(r.URL.Path, "/vote/")
	if roomID == "" {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	cookie, err := r.Cookie(cookiePrefix + roomID)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	value := strings.TrimSpace(r.FormValue("value"))
	if value == "" {
		http.Error(w, "Vote value required", http.StatusBadRequest)
		return
	}

	switch value {
	case "0", "1", "2", "3", "5", "8", "13", "21", "?", "☕":
		// Valid value
	default:
		http.Error(w, "Invalid vote value", http.StatusBadRequest)
		return
	}

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

	roomID := strings.TrimPrefix(r.URL.Path, "/reveal/")
	if roomID == "" {
		http.Error(w, "Room not found", http.StatusNotFound)
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

	roomID := strings.TrimPrefix(r.URL.Path, "/reset/")
	if roomID == "" {
		http.Error(w, "Room not found", http.StatusNotFound)
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
	roomID := strings.TrimPrefix(r.URL.Path, "/stream/")
	if roomID == "" {
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	sendSnapshot := func() error {
		room, err := storeEngine.RoomSnapshot(roomID)
		if err != nil {
			return err
		}

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

	if err := sendSnapshot(); err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		log.Println("Failed to send initial stream payload:", err)
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
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

func handleLogout(w http.ResponseWriter, r *http.Request) {
	roomID := strings.TrimPrefix(r.URL.Path, "/logout/")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	if cookie, err := r.Cookie(cookiePrefix + roomID); err == nil && cookie.Value != "" {
		if err := storeEngine.RemovePlayer(roomID, cookie.Value); err != nil && !errors.Is(err, store.ErrRoomNotFound) && !errors.Is(err, store.ErrPlayerNotFound) {
			log.Println("Failed to remove player from room:", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookiePrefix + roomID,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	renderTemplate(w, "logout", map[string]interface{}{"RoomID": roomID})

}

// watchForShutdown intercepts SIGINT/SIGTERM to flush in-memory rooms for durability.
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

func generateUUID() string {
	return uuid.NewString()
}

func setSessionCookie(w http.ResponseWriter, roomID, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookiePrefix + roomID,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 7,
	})
}

func renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	err := templates.ExecuteTemplate(w, name+".html", data)
	if err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		log.Println("Template execution error:", err)
	}
}
