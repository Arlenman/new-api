package common

import "sync"

type ImagePlaygroundStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"built_at"`
}

var imagePlaygroundStatusState struct {
	sync.RWMutex
	status ImagePlaygroundStatus
}

func SetImagePlaygroundStatus(status ImagePlaygroundStatus) {
	imagePlaygroundStatusState.Lock()
	imagePlaygroundStatusState.status = status
	imagePlaygroundStatusState.Unlock()
}

func GetImagePlaygroundStatus() ImagePlaygroundStatus {
	imagePlaygroundStatusState.RLock()
	defer imagePlaygroundStatusState.RUnlock()
	return imagePlaygroundStatusState.status
}
