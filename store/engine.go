package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// StorageDirEnvVar overrides where room snapshots are persisted on disk.
	StorageDirEnvVar = "SCRUMPOKER_STORAGE_DIR"

	// DefaultStorageDir is used when no override is provided.
	DefaultStorageDir = "./database"
)

var (
	// ErrRoomNotFound indicates the requested room is absent from the cache or disk.
	ErrRoomNotFound = errors.New("room not found")

	// ErrPlayerNotFound indicates the given session ID is not registered in the room.
	ErrPlayerNotFound = errors.New("player not found")
)

// StoreEngine keeps room state cached in memory and mirrors it to disk when changes occur.
type StoreEngine struct {
	baseDir string
	mu      sync.RWMutex
	rooms   map[string]*RoomState
}

// RoomState represents a Scrum Poker room with player roster and reveal status.
type RoomState struct {
	ID         string                  `json:"id"`
	CreatedAt  time.Time               `json:"created_at"`
	UpdatedAt  time.Time               `json:"updated_at"`
	Revealed   bool                    `json:"revealed"`
	Players    map[string]*PlayerState `json:"players"`
	GameMaster string                  `json:"game_master"`
	Dirty      bool                    `json:"-"`
}

// PlayerState captures an individual user's vote and role within a room.
type PlayerState struct {
	SessionID    string     `json:"session_id"`
	Name         string     `json:"name"`
	Vote         string     `json:"vote,omitempty"`
	VotedAt      *time.Time `json:"voted_at,omitempty"`
	IsGameMaster bool       `json:"is_game_master,omitempty"`
}

// NewStoreEngine initializes the store, hydrates state from disk, and ensures the persistence directory exists.
func NewStoreEngine(dir string) (*StoreEngine, error) {
	if dir == "" {
		dir = DefaultStorageDir
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, err
	}

	engine := &StoreEngine{
		baseDir: absDir,
		rooms:   make(map[string]*RoomState),
	}

	if err := engine.bootstrap(); err != nil {
		return nil, err
	}

	return engine, nil
}

// bootstrap loads all JSON room snapshots from disk into memory and normalizes their structure.
func (s *StoreEngine) bootstrap() error {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		roomID := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		statePath := filepath.Join(s.baseDir, entry.Name())

		data, err := os.ReadFile(statePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read %s: %w", statePath, err)
		}

		var room RoomState
		if err := json.Unmarshal(data, &room); err != nil {
			return fmt.Errorf("decode %s: %w", statePath, err)
		}

		if room.ID == "" {
			room.ID = roomID
		}
		if room.Players == nil {
			room.Players = make(map[string]*PlayerState)
		}

		if room.GameMaster == "" {
			for sessionID, player := range room.Players {
				if player != nil && player.IsGameMaster {
					room.GameMaster = sessionID
					break
				}
			}
		}

		if room.GameMaster == "" {
			for sessionID, player := range room.Players {
				if player != nil {
					room.GameMaster = sessionID
					player.IsGameMaster = true
					break
				}
			}
		}

		if room.GameMaster != "" {
			if gmPlayer, ok := room.Players[room.GameMaster]; ok && gmPlayer != nil {
				gmPlayer.IsGameMaster = true
			}
		}

		room.Dirty = false
		s.rooms[room.ID] = &room
	}

	return nil
}

// CreateRoom registers a new room, making the creator the game master and persisting the initial state.
func (s *StoreEngine) CreateRoom(roomID, sessionID, ownerName string) error {
	// Hold the write lock so in-memory room mutations and disk persistence stay atomic.
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[roomID]; exists {
		return fmt.Errorf("room %s already exists", roomID)
	}

	now := time.Now().UTC()
	if ownerName == "" {
		ownerName = "Game Master"
	}

	room := &RoomState{
		ID:         roomID,
		CreatedAt:  now,
		UpdatedAt:  now,
		Players:    make(map[string]*PlayerState),
		GameMaster: sessionID,
	}

	room.Players[sessionID] = &PlayerState{SessionID: sessionID, Name: ownerName, IsGameMaster: true}
	room.Dirty = true
	s.rooms[roomID] = room

	// persistRoomLocked assumes the caller already holds s.mu to keep state consistent.
	return s.persistRoomLocked(room)
}

// JoinRoom adds or updates a participant and ensures their latest display name is stored.
func (s *StoreEngine) JoinRoom(roomID, sessionID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return err
	}

	if name == "" {
		name = "Player"
	}

	if existing, ok := room.Players[sessionID]; ok {
		existing.Name = name
	} else {
		room.Players[sessionID] = &PlayerState{SessionID: sessionID, Name: name, IsGameMaster: false}
	}

	room.Dirty = true

	return s.persistRoomLocked(room)
}

// RegisterVote stores a player's latest estimate and marks the room as needing persistence.
func (s *StoreEngine) RegisterVote(roomID, sessionID, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return err
	}

	player, ok := room.Players[sessionID]
	if !ok {
		return ErrPlayerNotFound
	}

	now := time.Now().UTC()
	player.Vote = value
	timestamp := now
	player.VotedAt = &timestamp

	room.Revealed = false
	room.Dirty = true

	return s.persistRoomLocked(room)
}

// ResetVotes clears every player's estimate and timestamps while keeping the roster intact.
func (s *StoreEngine) ResetVotes(roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return err
	}

	for _, player := range room.Players {
		player.Vote = ""
		player.VotedAt = nil
	}

	room.Revealed = false
	room.UpdatedAt = time.Now().UTC()
	room.Dirty = true

	return s.persistRoomLocked(room)
}

// SetReveal flips the reveal flag so clients know whether votes should be visible.
func (s *StoreEngine) SetReveal(roomID string, revealed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return err
	}

	room.Revealed = revealed
	room.Dirty = true

	return s.persistRoomLocked(room)
}

// RemovePlayer deletes a participant from the roster and persists the new state.
func (s *StoreEngine) RemovePlayer(roomID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return err
	}

	if _, ok := room.Players[sessionID]; !ok {
		return ErrPlayerNotFound
	}

	delete(room.Players, sessionID)
	room.Dirty = true

	return s.persistRoomLocked(room)
}

// IsGameMaster quickly checks whether the provided session controls reveal/reset actions within the room.
func (s *StoreEngine) IsGameMaster(roomID, sessionID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return false, err
	}

	return room.GameMaster == sessionID, nil
}

// Player returns a safe copy of the requested participant so callers cannot mutate shared state.
func (s *StoreEngine) Player(roomID, sessionID string) (*PlayerState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return nil, err
	}

	player, ok := room.Players[sessionID]
	if !ok {
		return nil, ErrPlayerNotFound
	}

	return player.clone(), nil
}

// RoomSnapshot returns a deep copy so callers cannot mutate the cached room directly.
func (s *StoreEngine) RoomSnapshot(roomID string) (*RoomState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, err := s.getRoomUnsafe(roomID)
	if err != nil {
		return nil, err
	}

	return room.clone(), nil
}

// FlushAll forces every dirty room to be written to disk, useful during shutdown.
func (s *StoreEngine) FlushAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, room := range s.rooms {
		room.Dirty = true
		if err := s.persistRoomLocked(room); err != nil {
			return err
		}
	}

	return nil
}

// getRoomUnsafe assumes the caller already holds a lock and fetches a room reference.
func (s *StoreEngine) getRoomUnsafe(roomID string) (*RoomState, error) {
	room, ok := s.rooms[roomID]
	if !ok {
		return nil, ErrRoomNotFound
	}
	return room, nil
}

// persistRoomLocked atomically writes room state to disk using a write-and-rename pattern.
// This function assumes the caller already holds s.mu to prevent concurrent modifications.
//
// Data Storage Mechanism:
// Each room is stored as a separate JSON file in the baseDir directory.
// The filename format is: <room-id>.json
//
// Atomic Write Pattern:
// To ensure crash safety and prevent data corruption, we use an atomic write-and-rename approach:
//  1. Create a temporary file in the same directory with a .tmp extension
//  2. Write the complete JSON payload to the temporary file
//  3. Close the temporary file to flush all buffers
//  4. Atomically rename the temp file to the final filename
//     (on POSIX systems, rename is atomic and replaces the destination if it exists)
//  5. Set file timestamps to match room metadata
//
// This guarantees that readers never see a partially written file, even if the process
// crashes or is killed mid-write. The worst case is that the old version remains intact.
func (s *StoreEngine) persistRoomLocked(room *RoomState) error {
	// Skip persistence if the room is nil or has no pending changes
	if room == nil || !room.Dirty {
		return nil
	}

	// Initialize UpdatedAt on first persist (e.g., during CreateRoom) or if somehow unset.
	// Most callers set this explicitly before marking Dirty=true, so this is a safety net.
	if room.UpdatedAt.IsZero() {
		room.UpdatedAt = time.Now().UTC()
	}

	// Serialize the room state to JSON format
	payload, err := json.Marshal(room)
	if err != nil {
		return err
	}

	// Create a temporary file in the same directory as the final destination.
	// The temp file pattern includes the room ID for easier debugging.
	tmpFile, err := os.CreateTemp(s.baseDir, fmt.Sprintf("%s-*.tmp", room.ID))
	if err != nil {
		return err
	}

	// Write the JSON payload to the temporary file
	if _, err := tmpFile.Write(payload); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name()) // Clean up on failure
		return err
	}

	// Close the file to ensure all buffers are flushed to disk
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name()) // Clean up on failure
		return err
	}

	// Atomically move the temp file to its final location.
	// On Unix-like systems, rename() is atomic. If the destination exists, it's replaced.
	finalPath := s.roomFilePath(room.ID)
	if err := os.Rename(tmpFile.Name(), finalPath); err != nil {
		os.Remove(tmpFile.Name()) // Clean up on failure
		return err
	}

	// Synchronize file system timestamps with room metadata:
	// - Access time (atime) is set to room.CreatedAt (when the room was first created)
	// - Modification time (mtime) is set to room.UpdatedAt (when the room was last changed)
	//
	// This ensures the file metadata on disk reflects the logical creation/modification
	// times from the application's perspective, not just when the file was written.
	if err := os.Chtimes(finalPath, room.CreatedAt, room.UpdatedAt); err != nil {
		return err
	}

	// Clear the dirty flag to indicate the room state is synchronized with disk
	room.Dirty = false
	return nil
}

// roomFilePath returns the absolute path for a room's backing JSON file.
func (s *StoreEngine) roomFilePath(roomID string) string {
	return filepath.Join(s.baseDir, fmt.Sprintf("%s.json", roomID))
}

func (r *RoomState) clone() *RoomState {
	if r == nil {
		return nil
	}

	clonedPlayers := make(map[string]*PlayerState, len(r.Players))
	for sessionID, player := range r.Players {
		clonedPlayers[sessionID] = player.clone()
	}

	return &RoomState{
		ID:         r.ID,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
		Revealed:   r.Revealed,
		Players:    clonedPlayers,
		GameMaster: r.GameMaster,
	}
}

func (p *PlayerState) clone() *PlayerState {
	if p == nil {
		return nil
	}

	clone := *p
	if p.VotedAt != nil {
		ts := *p.VotedAt
		clone.VotedAt = &ts
	}
	return &clone
}

// SanitizedCopy hides other players' votes unless the room has been revealed and can optionally mask session IDs.
func (r *RoomState) SanitizedCopy(viewerSessionID string, maskSessions bool) *RoomState {
	clone := r.clone()
	if clone == nil {
		return nil
	}

	if !clone.Revealed {
		for sessionID, player := range clone.Players {
			if sessionID != viewerSessionID {
				player.Vote = ""
			}
		}
	}

	if maskSessions {
		maskedPlayers := make(map[string]*PlayerState, len(clone.Players))
		for sessionID, player := range clone.Players {
			maskedID := MaskSessionID(sessionID)
			player.SessionID = maskedID
			maskedPlayers[maskedID] = player
		}
		clone.Players = maskedPlayers
		clone.GameMaster = MaskSessionID(clone.GameMaster)
	}

	return clone
}

// MaskSessionID deterministically maps a session token to a UUID so clients can correlate players without exposing raw IDs.
func MaskSessionID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	masked := uuid.NewSHA1(uuid.NameSpaceOID, []byte(sessionID))
	return masked.String()
}
