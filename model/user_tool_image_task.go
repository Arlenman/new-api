package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	UserToolImageTaskStatusPending   = "pending"
	UserToolImageTaskStatusRunning   = "running"
	UserToolImageTaskStatusCompleted = "completed"
	UserToolImageTaskStatusFailed    = "failed"
	UserToolImageTaskStatusCancelled = "cancelled"

	UserToolImageTaskDefaultMaxAttempts = 3
	MaxUserToolImageTaskClientIDLength  = MaxUserToolClientMutationIDLength
	MaxUserToolImageTaskErrorLength     = 4096
)

var (
	ErrUserToolImageTaskIdempotencyConflict = errors.New("user tool image task idempotency conflict")
	ErrUserToolImageTaskLeaseLost           = errors.New("user tool image task lease lost")
)

// UserToolImageTask persists an image-generation request independently from the
// HTTP request that created it. RequestSnapshot must contain only the filtered
// request needed to replay the generation; provider credentials never belong in
// this record.
type UserToolImageTask struct {
	TaskID           string    `json:"task_id" gorm:"primaryKey;type:varchar(64)"`
	UserID           int       `json:"user_id" gorm:"index:idx_user_tool_image_task_owner,priority:1;uniqueIndex:uq_user_tool_image_task_client,priority:1"`
	Tool             string    `json:"tool" gorm:"type:varchar(64);index:idx_user_tool_image_task_owner,priority:2;uniqueIndex:uq_user_tool_image_task_client,priority:2"`
	ClientTaskID     string    `json:"client_task_id" gorm:"type:varchar(128);uniqueIndex:uq_user_tool_image_task_client,priority:3"`
	SessionID        string    `json:"session_id" gorm:"type:varchar(255);index"`
	MessageKey       string    `json:"message_key" gorm:"type:varchar(255);index"`
	TokenID          int       `json:"token_id" gorm:"index"`
	RequestSnapshot  JSONValue `json:"request_snapshot" gorm:"type:text"`
	RequestHash      string    `json:"request_hash" gorm:"type:char(64)"`
	Status           string    `json:"status" gorm:"type:varchar(32);index"`
	Attempt          int       `json:"attempt"`
	LeaseOwner       string    `json:"lease_owner" gorm:"type:varchar(128);index"`
	LeaseUntil       int64     `json:"lease_until" gorm:"bigint;index"`
	BillingStatus    string    `json:"billing_status" gorm:"type:varchar(32);index"`
	BillingRequestID string    `json:"billing_request_id" gorm:"type:varchar(128);index"`
	PreConsumedQuota int64     `json:"pre_consumed_quota"`
	SettledQuota     int64     `json:"settled_quota"`
	RefundedQuota    int64     `json:"refunded_quota"`
	ResultItemID     string    `json:"result_item_id" gorm:"type:varchar(255);index"`
	ErrorCode        string    `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage     string    `json:"error_message" gorm:"type:text"`
	CreatedAt        int64     `json:"created_at" gorm:"bigint;index"`
	UpdatedAt        int64     `json:"updated_at" gorm:"bigint;index"`
	StartedAt        int64     `json:"started_at" gorm:"bigint;index"`
	FinishedAt       int64     `json:"finished_at" gorm:"bigint;index"`
}

// UserToolImageTaskInput is the server-side, credential-free task envelope.
// The API layer is responsible for filtering the client request before passing
// it here and for validating the selected token belongs to the current user.
type UserToolImageTaskInput struct {
	UserID          int
	Tool            string
	ClientTaskID    string
	SessionID       string
	MessageKey      string
	TokenID         int
	RequestSnapshot JSONValue
}

func (task *UserToolImageTask) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().UnixMilli()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = now
	}
	if task.Status == "" {
		task.Status = UserToolImageTaskStatusPending
	}
	if task.BillingRequestID == "" {
		task.BillingRequestID = task.TaskID
	}
	return nil
}

func CreateOrGetUserToolImageTask(input UserToolImageTaskInput) (*UserToolImageTask, bool, error) {
	if err := validateUserToolImageTaskInput(input); err != nil {
		return nil, false, err
	}

	snapshot := input.RequestSnapshot
	if len(snapshot) == 0 {
		snapshot = JSONValue(`{}`)
	}
	requestHash, err := hashUserToolImageTaskRequest(input, snapshot)
	if err != nil {
		return nil, false, err
	}

	task := &UserToolImageTask{}
	existingTask := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		var existing UserToolImageTask
		lookupErr := lockForUpdate(tx).Where(
			"user_id = ? AND tool = ? AND client_task_id = ?",
			input.UserID, input.Tool, input.ClientTaskID,
		).First(&existing).Error
		if lookupErr == nil {
			if existing.RequestHash != requestHash {
				return ErrUserToolImageTaskIdempotencyConflict
			}
			*task = existing
			existingTask = true
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}

		task = &UserToolImageTask{
			TaskID:          "uitask_" + common.GetUUID(),
			UserID:          input.UserID,
			Tool:            input.Tool,
			ClientTaskID:    input.ClientTaskID,
			SessionID:       strings.TrimSpace(input.SessionID),
			MessageKey:      strings.TrimSpace(input.MessageKey),
			TokenID:         input.TokenID,
			RequestSnapshot: snapshot,
			RequestHash:     requestHash,
			Status:          UserToolImageTaskStatusPending,
			BillingStatus:   "pending",
		}
		return tx.Create(task).Error
	})
	if err == nil {
		return task, existingTask, nil
	}

	// A concurrent request can win the unique idempotency key while this
	// transaction rolls back. Read the committed winner and apply the same hash
	// check so retries remain deterministic on all supported databases.
	var existing UserToolImageTask
	lookupErr := DB.Where(
		"user_id = ? AND tool = ? AND client_task_id = ?",
		input.UserID, input.Tool, input.ClientTaskID,
	).First(&existing).Error
	if lookupErr == nil {
		if existing.RequestHash != requestHash {
			return nil, false, ErrUserToolImageTaskIdempotencyConflict
		}
		return &existing, true, nil
	}
	return nil, false, err
}

func GetUserToolImageTask(userID int, taskID string) (*UserToolImageTask, error) {
	if userID <= 0 || strings.TrimSpace(taskID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var task UserToolImageTask
	if err := DB.Where("task_id = ? AND user_id = ?", strings.TrimSpace(taskID), userID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func ListReadyUserToolImageTasks(limit int, now int64) ([]UserToolImageTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	var tasks []UserToolImageTask
	err := DB.Where(
		"(status = ? AND attempt < ?) OR (status = ? AND lease_until < ?)",
		UserToolImageTaskStatusPending,
		UserToolImageTaskDefaultMaxAttempts,
		UserToolImageTaskStatusRunning,
		now,
	).Order("created_at ASC, task_id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// ClaimUserToolImageTask atomically changes a pending task, or a task whose
// lease expired, to running. The lease owner is required for every later
// state transition so a stale worker cannot publish a result.
func ClaimUserToolImageTask(taskID, workerID string, leaseUntil, now int64) (*UserToolImageTask, bool, error) {
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(workerID) == "" || leaseUntil <= now {
		return nil, false, errors.New("invalid user tool image task lease")
	}

	var task UserToolImageTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("task_id = ?", taskID).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if isTerminalUserToolImageTaskStatus(task.Status) {
			return nil
		}
		if task.Status == UserToolImageTaskStatusRunning && task.LeaseUntil >= now {
			return nil
		}
		if task.Attempt >= UserToolImageTaskDefaultMaxAttempts {
			result := tx.Model(&UserToolImageTask{}).Where(
				"task_id = ? AND status IN ?",
				task.TaskID, []string{UserToolImageTaskStatusPending, UserToolImageTaskStatusRunning},
			).Updates(map[string]any{
				"status":        UserToolImageTaskStatusFailed,
				"error_code":    "max_attempts_exceeded",
				"error_message": "maximum retry attempts exceeded",
				"lease_owner":   "",
				"lease_until":   0,
				"finished_at":   now,
				"updated_at":    now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 1 {
				task.Status = UserToolImageTaskStatusFailed
				task.ErrorCode = "max_attempts_exceeded"
				task.ErrorMessage = "maximum retry attempts exceeded"
				task.LeaseOwner = ""
				task.LeaseUntil = 0
				task.FinishedAt = now
				task.UpdatedAt = now
			}
			return nil
		}

		updates := map[string]any{
			"status":      UserToolImageTaskStatusRunning,
			"attempt":     task.Attempt + 1,
			"lease_owner": strings.TrimSpace(workerID),
			"lease_until": leaseUntil,
			"updated_at":  now,
		}
		if task.StartedAt == 0 {
			updates["started_at"] = now
		}
		result := tx.Model(&UserToolImageTask{}).Where(
			"task_id = ? AND (status = ? OR (status = ? AND lease_until < ?))",
			taskID, UserToolImageTaskStatusPending, UserToolImageTaskStatusRunning, now,
		).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			task.TaskID = ""
			return nil
		}
		return tx.Where("task_id = ?", taskID).First(&task).Error
	})
	if err != nil {
		return nil, false, err
	}
	if task.TaskID == "" {
		return nil, false, nil
	}
	return &task, task.Status == UserToolImageTaskStatusRunning && task.LeaseOwner == strings.TrimSpace(workerID), nil
}

func RenewUserToolImageTaskLease(taskID, workerID string, leaseUntil, now int64) error {
	if leaseUntil <= now || strings.TrimSpace(taskID) == "" || strings.TrimSpace(workerID) == "" {
		return ErrUserToolImageTaskLeaseLost
	}
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	result := DB.Model(&UserToolImageTask{}).Where(
		"task_id = ? AND status = ? AND lease_owner = ? AND lease_until >= ?",
		taskID, UserToolImageTaskStatusRunning, strings.TrimSpace(workerID), now,
	).Updates(map[string]any{"lease_until": leaseUntil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserToolImageTaskLeaseLost
	}
	return nil
}

func CompleteUserToolImageTask(taskID, workerID, resultItemID string, now int64) error {
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	result := DB.Model(&UserToolImageTask{}).Where(
		"task_id = ? AND status = ? AND lease_owner = ? AND lease_until >= ?",
		taskID, UserToolImageTaskStatusRunning, strings.TrimSpace(workerID), now,
	).Updates(map[string]any{
		"status":         UserToolImageTaskStatusCompleted,
		"result_item_id": strings.TrimSpace(resultItemID),
		"lease_owner":    "",
		"lease_until":    0,
		"finished_at":    now,
		"updated_at":     now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserToolImageTaskLeaseLost
	}
	return nil
}

func FailUserToolImageTask(taskID, workerID, errorCode, errorMessage string, now int64) (*UserToolImageTask, error) {
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	errorCode = strings.TrimSpace(errorCode)
	errorMessage = strings.TrimSpace(errorMessage)
	if len(errorCode) > 128 {
		errorCode = errorCode[:128]
	}
	if len(errorMessage) > MaxUserToolImageTaskErrorLength {
		errorMessage = errorMessage[:MaxUserToolImageTaskErrorLength]
	}

	var task UserToolImageTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("task_id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		if task.Status != UserToolImageTaskStatusRunning || task.LeaseOwner != strings.TrimSpace(workerID) || task.LeaseUntil < now {
			return ErrUserToolImageTaskLeaseLost
		}
		status := UserToolImageTaskStatusPending
		finishedAt := int64(0)
		if task.Attempt >= UserToolImageTaskDefaultMaxAttempts {
			status = UserToolImageTaskStatusFailed
			finishedAt = now
		}
		result := tx.Model(&UserToolImageTask{}).Where(
			"task_id = ? AND status = ? AND lease_owner = ? AND lease_until >= ?",
			taskID, UserToolImageTaskStatusRunning, strings.TrimSpace(workerID), now,
		).Updates(map[string]any{
			"status":        status,
			"error_code":    errorCode,
			"error_message": errorMessage,
			"lease_owner":   "",
			"lease_until":   0,
			"finished_at":   finishedAt,
			"updated_at":    now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrUserToolImageTaskLeaseLost
		}
		return tx.Where("task_id = ?", taskID).First(&task).Error
	})
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func CancelUserToolImageTask(userID int, taskID string, now int64) (bool, error) {
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	result := DB.Model(&UserToolImageTask{}).Where(
		"task_id = ? AND user_id = ? AND status IN ?",
		taskID, userID, []string{UserToolImageTaskStatusPending, UserToolImageTaskStatusRunning},
	).Updates(map[string]any{
		"status":      UserToolImageTaskStatusCancelled,
		"lease_owner": "",
		"lease_until": 0,
		"finished_at": now,
		"updated_at":  now,
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func validateUserToolImageTaskInput(input UserToolImageTaskInput) error {
	if input.UserID <= 0 || input.Tool != UserToolImagePlayground {
		return errors.New("invalid user tool image task")
	}
	if strings.TrimSpace(input.ClientTaskID) == "" || len(input.ClientTaskID) > MaxUserToolImageTaskClientIDLength {
		return errors.New("invalid user tool image task client id")
	}
	if len(input.RequestSnapshot) > MaxUserToolMutationPayloadBytes {
		return fmt.Errorf("user tool image task request is too large")
	}
	if len(input.RequestSnapshot) > 0 {
		var decoded any
		if err := common.Unmarshal(input.RequestSnapshot, &decoded); err != nil {
			return errors.New("invalid user tool image task request snapshot")
		}
	}
	return nil
}

func hashUserToolImageTaskRequest(input UserToolImageTaskInput, snapshot JSONValue) (string, error) {
	encoded, err := common.Marshal(struct {
		Tool            string    `json:"tool"`
		ClientTaskID    string    `json:"client_task_id"`
		SessionID       string    `json:"session_id"`
		MessageKey      string    `json:"message_key"`
		TokenID         int       `json:"token_id"`
		RequestSnapshot JSONValue `json:"request_snapshot"`
	}{
		Tool:            input.Tool,
		ClientTaskID:    strings.TrimSpace(input.ClientTaskID),
		SessionID:       strings.TrimSpace(input.SessionID),
		MessageKey:      strings.TrimSpace(input.MessageKey),
		TokenID:         input.TokenID,
		RequestSnapshot: snapshot,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func isTerminalUserToolImageTaskStatus(status string) bool {
	return status == UserToolImageTaskStatusCompleted || status == UserToolImageTaskStatusFailed || status == UserToolImageTaskStatusCancelled
}
