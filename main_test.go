package main

import (
	"os"
	"testing"
)

func TestTaskManager_Logic(t *testing.T) {
	testFile := "test_tasks.json"
	os.Remove(testFile)       // 確保測試開始前環境是乾淨的
	defer os.Remove(testFile) // 測試完後刪除暫存檔

	tm := NewTaskManager(testFile)

	// 1. 驗證新增任務功能
	t.Run("AddTask", func(t *testing.T) {
		tm.AddTask("喝水", "每天八杯水", "", "", "10:00", "daily", "")
		if len(tm.Tasks) != 1 {
			t.Errorf("預期任務數量為 1，得到 %d", len(tm.Tasks))
		}
		if tm.Tasks[0].Topic != "喝水" {
			t.Errorf("任務主題錯誤: %s", tm.Tasks[0].Topic)
		}
	})

	// 2. 驗證 ID 是否正確自動遞增
	t.Run("IDIncrement", func(t *testing.T) {
		tm.AddTask("開會", "週報會議", "", "2099-01-01", "11:00", "once", "")
		if tm.Tasks[1].ID != 2 {
			t.Errorf("預期 ID 為 2，得到 %d", tm.Tasks[1].ID)
		}
	})

	// 3. 驗證刪除任務功能
	t.Run("DeleteTask", func(t *testing.T) {
		success := tm.DeleteTask(1)
		if !success || len(tm.Tasks) != 1 {
			t.Errorf("刪除任務 ID:1 失敗")
		}
		if tm.Tasks[0].ID != 2 {
			t.Errorf("剩餘任務 ID 錯誤，預期為 2")
		}
	})

	// 4. 驗證資料持久化（存檔與讀取）
	t.Run("Persistence", func(t *testing.T) {
		tm2 := NewTaskManager(testFile)
		if len(tm2.Tasks) != 1 || tm2.Tasks[0].Topic != "開會" {
			t.Error("資料持久化讀取失敗，存檔可能未正確寫入")
		}
	})
}
