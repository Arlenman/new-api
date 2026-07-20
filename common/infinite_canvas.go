package common

import "sync"

type InfiniteCanvasStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"built_at"`
}

var infiniteCanvasStatusState struct {
	sync.RWMutex
	status InfiniteCanvasStatus
}

func SetInfiniteCanvasStatus(status InfiniteCanvasStatus) {
	infiniteCanvasStatusState.Lock()
	infiniteCanvasStatusState.status = status
	infiniteCanvasStatusState.Unlock()
}

func GetInfiniteCanvasStatus() InfiniteCanvasStatus {
	infiniteCanvasStatusState.RLock()
	defer infiniteCanvasStatusState.RUnlock()
	return infiniteCanvasStatusState.status
}
