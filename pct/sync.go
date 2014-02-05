package pct

type SyncChan struct {
	StartChan chan bool
	StopChan  chan bool
	DoneChan  chan bool
	CrashChan chan bool
	Crash     bool
}

func NewSyncChan() *SyncChan {
	sc := &SyncChan{
		StartChan: make(chan bool),
		StopChan:  make(chan bool),
		DoneChan:  make(chan bool, 1),
		CrashChan: make(chan bool, 1),
		Crash:     true,
	}
	return sc
}

func (sync *SyncChan) Start() bool {
	started := false
	select {
	case sync.StartChan <- true:
		started = <-sync.StartChan
	default:
	}
	return started
}

func (sync *SyncChan) Stop() {
	sync.StopChan <- true
}

func (sync *SyncChan) Wait() {
	select {
	case <-sync.CrashChan:
	case <-sync.DoneChan:
	}
}

func (sync *SyncChan) Done() {
	if sync.Crash {
		sync.CrashChan <- true
	} else {
		sync.DoneChan <- true
	}
}

func (sync *SyncChan) Graceful() {
	sync.Crash = false
}

func (sync *SyncChan) IsGraceful() bool {
	return !sync.Crash
}
