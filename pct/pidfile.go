package pct

import (
	"fmt"
	"os"
	"sync"
)

type PidFile struct {
	name string
	mux  *sync.RWMutex
}

func NewPidFile() *PidFile {
	p := &PidFile{
		mux: new(sync.RWMutex),
	}
	return p
}

func (p *PidFile) Get() string {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.name
}

func (p *PidFile) Set(pidFile string) error {
	/**
	 * Get new PID file _then_ remove old, i.e. don't give up current PID file
	 * until new PID file is secured.  If no new PID file, then just remove old
	 * (if any) and return early.
	 */
	p.mux.Lock()
	defer p.mux.Unlock()

	// Return early if no new PID file.
	if pidFile == "" {
		// Remove existing PID file if any.  Do NOT call Remove() because it locks.
		if err := p.remove(); err != nil {
			return err
		}
		return nil
	}

	// Create new PID file, success only if it doesn't already exist.
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	file, err := os.OpenFile(pidFile, flags, 0644)
	if err != nil {
		return err
	}

	// Write PID to new PID file and close.
	if _, err := file.WriteString(fmt.Sprintf("%d\n", os.Getpid())); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	// Remove old PID file if any.  Do NOT call Remove() because it locks.
	if err := p.remove(); err != nil {
		// If this happens we're in a weird state: holding both PID files.
		return err
	}

	// Success: new PID file set, old removed.
	p.name = pidFile
	return nil
}

func (p *PidFile) Remove() error {
	p.mux.Lock()
	defer p.mux.Unlock()
	if p.name == "" {
		return nil
	}
	return p.remove()
}

func (p *PidFile) remove() error {
	// Do NOT lock here.  Expect caller to lock.
	if err := os.Remove(p.name); err != nil && !os.IsNotExist(err) {
		return err
	}
	p.name = ""
	return nil
}
