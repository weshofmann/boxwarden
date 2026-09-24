package session

import (
	"fmt"
	"os"
)

// RemoveDeletingRecordLocked removes only the exact deletion reservation.
// The caller holds the session and storage locks, has freshly proved the
// backend absent, and has cleared all exact workspace attachments.
func RemoveDeletingRecordLocked(stateRoot string, expected Record) error {
	if expected.Version != recordVersion || expected.IntendedState != StateDeleting || expected.StartGeneration != "" {
		return fmt.Errorf("invalid deleting session record")
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	sessions, err := openSessionChild(root, "sessions", false)
	if err != nil {
		return err
	}
	defer sessions.Close()
	current, err := loadRecordFromRoot(sessions, expected.Domain, expected.Name)
	if err != nil || current != expected {
		return fmt.Errorf("deleting session record changed: %v", err)
	}
	name := string(expected.Name) + ".json"
	info, err := sessions.Lstat(name)
	if err != nil {
		return err
	}
	if err := requirePrivateRegularInfo(info); err != nil {
		return err
	}
	file, err := openSessionPrivateRegular(sessions, name)
	if err != nil {
		return err
	}
	opened, err := file.Stat()
	closeErr := file.Close()
	if err != nil || closeErr != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("deleting session record changed while pinned: stat %v close %v", err, closeErr)
	}
	if err := sessions.Remove(name); err != nil {
		return err
	}
	return sessionSyncRoot(sessions)
}
