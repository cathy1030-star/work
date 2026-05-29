package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Task 定義工作項目結構
type Task struct {
	ID               int    `json:"id"`
	Topic            string `json:"topic"`              // 任務主題
	Name             string `json:"name,omitempty"`     // 舊欄位：用於相容舊資料
	Content          string `json:"content"`            // 任務內容
	Notes            string `json:"notes"`              // 備註事項
	Date             string `json:"date"`               // 新增日期: "2006-01-02" (YYYY-MM-DD)
	Time             string `json:"time"`               // 格式範例: "15:04"
	LastNotifiedDate string `json:"last_notified_date"` // 記錄上次提醒日期: "2006-01-02"
	RecurrenceType   string `json:"recurrence_type"`    // "once", "daily", "weekly", "monthly"
	RecurrenceDetail string `json:"recurrence_detail"`  // For "weekly": "Monday"; for "monthly": "15"
	IsPending        bool   `json:"is_pending"`         // 是否正在等待執行確認
}

// TaskManager 管理任務邏輯與持久化
type TaskManager struct {
	Tasks    []Task
	filePath string
	mu       sync.Mutex
}

// NewTaskManager 初始化管理器
func NewTaskManager(path string) *TaskManager {
	// 自動取得執行檔所在路徑，確保產生的 exe 檔案能正確讀取同目錄下的 tasks.json
	exePath, err := os.Executable()
	if err == nil {
		path = filepath.Join(filepath.Dir(exePath), "tasks.json")
	}
	tm := &TaskManager{filePath: path}
	tm.load()
	return tm
}

// load 從檔案讀取資料
func (tm *TaskManager) load() {
	data, err := os.ReadFile(tm.filePath)
	if err == nil {
		if err := json.Unmarshal(data, &tm.Tasks); err != nil {
			fmt.Printf("⚠️ 讀取存檔時發生錯誤: %v\n", err)
		}
		// 舊資料相容遷移與初始化邏輯
		for i := range tm.Tasks {
			if tm.Tasks[i].Topic == "" && tm.Tasks[i].Name != "" {
				tm.Tasks[i].Topic = tm.Tasks[i].Name
				tm.Tasks[i].Name = "" // 轉移後清除舊欄位
			}
			// 處理舊資料缺少重複類型的情況
			if tm.Tasks[i].RecurrenceType == "" {
				if tm.Tasks[i].Date == "" {
					tm.Tasks[i].RecurrenceType = "daily"
				} else {
					tm.Tasks[i].RecurrenceType = "once"
				}
			}
		}

		// 驗證日期格式
		for _, t := range tm.Tasks {
			if t.Date != "" {
				if _, err := time.Parse("2006-01-02", t.Date); err != nil {
					fmt.Printf("⚠️ 警告：任務 ID:%d 的日期格式 [%s] 無效\n", t.ID, t.Date)
				}
			}
			// 驗證重複類型細節
			if t.RecurrenceType == "weekly" {
				// 檢查是否為有效的星期幾
				validWeekdays := map[string]bool{"Sunday": true, "Monday": true, "Tuesday": true, "Wednesday": true, "Thursday": true, "Friday": true, "Saturday": true}
				if !validWeekdays[t.RecurrenceDetail] {
					fmt.Printf("⚠️ 警告：任務 ID:%d 的每週重複細節 [%s] 無效\n", t.ID, t.RecurrenceDetail)
				}
			} else if t.RecurrenceType == "monthly" {
				if day, err := strconv.Atoi(t.RecurrenceDetail); err != nil || day < 1 || day > 31 {
					fmt.Printf("⚠️ 警告：任務 ID:%d 的每月重複細節 [%s] 無效\n", t.ID, t.RecurrenceDetail)
				}
			}
		}
		// 驗證存檔中的任務時間格式是否符合 15:04 (24小時制)
		for _, t := range tm.Tasks {
			if _, err := time.Parse("15:04", t.Time); err != nil {
				fmt.Printf("⚠️ 警告：任務 ID:%d 的時間格式 [%s] 無效\n", t.ID, t.Time)
			}
		}
	}
}

// saveLocked 執行實際的 JSON 存檔動作。
// 注意：此方法不具備鎖保護，呼叫者必須先持有 tm.mu 鎖。
func (tm *TaskManager) saveLocked() {
	data, err := json.MarshalIndent(tm.Tasks, "", "  ")
	if err != nil {
		fmt.Printf("⚠️ 資料序列化失敗: %v\n", err)
		return
	}
	if err := os.WriteFile(tm.filePath, data, 0644); err != nil {
		fmt.Printf("⚠️ 無法寫入存檔: %v\n", err)
	}
}

// Save 提供給外部呼叫的存檔方法，具備互斥鎖保護。
func (tm *TaskManager) Save() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.saveLocked()
}

// AddTask 將新的工作項目加入清單並存檔。
func (tm *TaskManager) AddTask(topic, content, notes, date, t, recurrenceType, recurrenceDetail string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.addTaskLocked(topic, content, notes, date, t, recurrenceType, recurrenceDetail)
}

// addTaskLocked 內部使用的不帶鎖版本
func (tm *TaskManager) addTaskLocked(topic, content, notes, date, t, recurrenceType, recurrenceDetail string) {
	maxID := 0
	for _, task := range tm.Tasks {
		if task.ID > maxID {
			maxID = task.ID
		}
	}
	task := Task{
		ID:               maxID + 1,
		Topic:            topic,
		Content:          content,
		Notes:            notes,
		Date:             date,
		Time:             t,
		RecurrenceType:   recurrenceType,
		RecurrenceDetail: recurrenceDetail,
	}
	tm.Tasks = append(tm.Tasks, task)
	tm.saveLocked()
}

// DeleteTask 根據 ID 移除特定的工作項目。
func (tm *TaskManager) DeleteTask(id int) bool {
	tm.mu.Lock()
	for i, task := range tm.Tasks {
		if task.ID == id {
			tm.Tasks = append(tm.Tasks[:i], tm.Tasks[i+1:]...)
			tm.saveLocked()
			tm.mu.Unlock()
			return true
		}
	}
	tm.mu.Unlock()
	return false
}

// StartNotifier 啟動背景監控協程，負責定時檢查並發送通知。
func (tm *TaskManager) StartNotifier() {
	for {
		now := time.Now().Format("15:04")
		today := time.Now().Format("2006-01-02")
		var tasksToNotify []string

		tm.mu.Lock()
		// 獲取當前日期和時間的詳細資訊
		currentTime := time.Now()
		currentWeekday := currentTime.Weekday().String()     // "Monday", "Tuesday", ...
		currentDayOfMonth := strconv.Itoa(currentTime.Day()) // "1", "2", ...

		for i := range tm.Tasks {
			shouldTrigger := false

			// 根據重複類型判斷是否觸發
			if tm.Tasks[i].LastNotifiedDate != today { // 確保今天尚未提醒過
				switch tm.Tasks[i].RecurrenceType {
				case "once":
					// 一次性任務：日期已過，或是日期是今天且時間已到
					isSpecificDateReached := tm.Tasks[i].Date != "" && tm.Tasks[i].Date <= today
					if isSpecificDateReached {
						if tm.Tasks[i].Date < today || now >= tm.Tasks[i].Time {
							shouldTrigger = true
						}
					}
				case "daily":
					// 每日任務：時間已到或已過
					if now >= tm.Tasks[i].Time {
						shouldTrigger = true
					}
				case "weekly":
					// 每週任務：星期幾符合且時間已到或已過
					if currentWeekday == tm.Tasks[i].RecurrenceDetail && now >= tm.Tasks[i].Time {
						shouldTrigger = true
					}
				case "monthly":
					// 每月任務：日期符合且時間已到或已過
					if currentDayOfMonth == tm.Tasks[i].RecurrenceDetail && now >= tm.Tasks[i].Time {
						shouldTrigger = true
					}
				}
			}

			if shouldTrigger {
				// 觸發後更新狀態
				tm.Tasks[i].LastNotifiedDate = today
				tm.Tasks[i].IsPending = true
				tasksToNotify = append(tasksToNotify, tm.Tasks[i].Topic)
			}
		}

		// 狀態更新後立即存檔以保持同步
		if len(tasksToNotify) > 0 {
			tm.saveLocked()
		}
		tm.mu.Unlock()

		// 在鎖之外發送通知，避免阻塞其他功能
		// 改用終端機列印提醒，不再依賴外部套件
		for _, name := range tasksToNotify {
			fmt.Printf("\n🔔 【工作提醒】現在時間 %s，該執行項目：%s\n", now, name)
		}

		// 每 30 秒檢查一次即可
		time.Sleep(30 * time.Second)
	}
}

// handleTasks 處理 API 請求
func (tm *TaskManager) handleTasks(w http.ResponseWriter, r *http.Request) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tm.Tasks)
	case http.MethodPost:
		var t Task
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 使用重構後的內部方法，避免代碼重複
		tm.addTaskLocked(t.Topic, t.Content, t.Notes, t.Date, t.Time, t.RecurrenceType, t.RecurrenceDetail)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(t)
	case http.MethodDelete:
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.Atoi(idStr)
		found := false
		for i, task := range tm.Tasks {
			if task.ID == id {
				tm.Tasks = append(tm.Tasks[:i], tm.Tasks[i+1:]...)
				tm.saveLocked()
				found = true
				break
			}
		}
		if found {
			w.WriteHeader(http.StatusOK)
		} else {
			http.Error(w, "Task not found", http.StatusNotFound)
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func main() {
	tm := NewTaskManager("tasks.json")
	scanner := bufio.NewScanner(os.Stdin)

	// 啟動背景提醒協程 (Goroutine)
	go tm.StartNotifier()

	// 設定網頁路由
	go func() {
		// API 介面
		http.HandleFunc("/api/tasks", tm.handleTasks)
		// 靜態檔案介面 (HTML/JS)
		fs := http.FileServer(http.Dir("."))
		http.Handle("/", fs)

		fmt.Println("🌐 網頁伺服器已啟動: http://localhost:8080")
		http.ListenAndServe(":8080", nil)
	}()

	fmt.Println("🚀 每日工作提醒工具已啟動")

	for {
		// 優先檢查是否有「待確認」的提醒任務
		var pendingID int = -1
		tm.mu.Lock()
		for i := range tm.Tasks {
			if tm.Tasks[i].IsPending {
				pendingID = tm.Tasks[i].ID
				fmt.Printf("\n⚠️  【確認任務狀態】主題: %s (內容: %s)\n", tm.Tasks[i].Topic, tm.Tasks[i].Content)
				break
			}
		}
		tm.mu.Unlock()

		if pendingID != -1 {
			fmt.Println("1. 處理中 (延期處理)")
			fmt.Println("2. 處理完畢 (刪除項目)")
			fmt.Print("請選擇狀態 (1-2): ")
			if scanner.Scan() {
				statusChoice := strings.TrimSpace(scanner.Text())
				if statusChoice == "1" {
					fmt.Print("請輸入延期天數 (0 表示今天, 1 表示明天...): ")
					var days int
					if scanner.Scan() {
						daysStr := strings.TrimSpace(scanner.Text())
						var err error
						days, err = strconv.Atoi(daysStr)
						if err != nil {
							fmt.Println("❌ 請輸入有效的數字天數")
							continue
						}
					}

					fmt.Print("請輸入新的延期提醒時間 (HH:MM): ")
					if scanner.Scan() {
						newTime := strings.TrimSpace(scanner.Text())
						if _, err := time.Parse("15:04", newTime); err == nil {
							newDate := time.Now().AddDate(0, 0, days).Format("2006-01-02")
							tm.mu.Lock()
							for i := range tm.Tasks {
								if tm.Tasks[i].ID == pendingID {
									tm.Tasks[i].Time = newTime
									tm.Tasks[i].Date = newDate
									tm.Tasks[i].IsPending = false
									tm.Tasks[i].LastNotifiedDate = ""   // 清除後讓新時間能再次觸發
									tm.Tasks[i].RecurrenceType = "once" // 延期後變為一次性任務
									tm.Tasks[i].RecurrenceDetail = ""
									break
								}
							}
							tm.saveLocked()
							tm.mu.Unlock()
							fmt.Printf("✅ 任務已成功延期至 %s %s\n", newDate, newTime)
						} else {
							fmt.Println("❌ 格式錯誤，延期失敗")
						}
					}
				} else if statusChoice == "2" {
					tm.DeleteTask(pendingID)
					fmt.Println("✅ 項目已標記完畢並刪除")
				} else {
					fmt.Println("⚠️  請先處理此待確認任務，輸入 1 或 2。")
				}
			}
			continue // 處理完待辦後重新回到迴圈開頭檢查下一個或顯示選單
		}

		fmt.Println("\n====================")
		fmt.Println("1. 新增工作項目")
		fmt.Println("2. 查看今日清單")
		fmt.Println("3. 查看本月清單 (未來 30 天)")
		fmt.Println("4. 刪除工作項目")
		fmt.Println("5. 生成 AI 助手分析提示詞")
		fmt.Println("6. 退出工具")
		fmt.Print("請選擇操作 (1-6) 並按 Enter: ")

		if !scanner.Scan() {
			break // 如果輸入流關閉，則退出程式
		}
		choice := strings.TrimSpace(scanner.Text())

		switch choice {
		case "1":
			fmt.Print("請輸入任務主題 (Topic): ")
			var topic string
			if scanner.Scan() {
				topic = strings.TrimSpace(scanner.Text())
			}
			if topic == "" {
				fmt.Println("❌ 主題不能為空")
				continue
			}

			fmt.Print("請輸入任務內容 (Content): ")
			var content string
			if scanner.Scan() {
				content = strings.TrimSpace(scanner.Text())
			}

			fmt.Print("請輸入備註事項 (Notes): ")
			var notes string
			if scanner.Scan() {
				notes = strings.TrimSpace(scanner.Text())
			}

			var date string
			var recurrenceType string
			var recurrenceDetail string

			fmt.Println("請選擇重複類型:")
			fmt.Println("  1. 一次性任務 (指定日期)")
			fmt.Println("  2. 每日重複")
			fmt.Println("  3. 每週重複 (指定星期幾)")
			fmt.Println("  4. 每月重複 (指定日期)")
			fmt.Print("請選擇 (1-4): ")
			if scanner.Scan() {
				recurrenceChoice := strings.TrimSpace(scanner.Text())
				switch recurrenceChoice {
				case "1":
					recurrenceType = "once"
					fmt.Print("請輸入日期 (YYYY-MM-DD): ")
					if scanner.Scan() {
						date = strings.TrimSpace(scanner.Text())
					}
					if _, err := time.Parse("2006-01-02", date); err != nil {
						fmt.Println("❌ 日期格式錯誤，請使用 YYYY-MM-DD (例如 2023-11-25)")
						continue
					}
				case "2":
					recurrenceType = "daily"
					date = "" // 每日重複不需要特定日期
				case "3":
					recurrenceType = "weekly"
					date = "" // 每週重複不需要特定日期
					fmt.Print("請輸入星期幾 (1-7、一-日 或 Monday，支援帶點號如 3.): ")
					var input string
					if scanner.Scan() {
						// 移除前後空白及結尾可能的點號
						input = strings.TrimRight(strings.TrimSpace(scanner.Text()), ".")
					}

					weekdayMap := map[string]string{
						"1": "Monday", "2": "Tuesday", "3": "Wednesday", "4": "Thursday", "5": "Friday", "6": "Saturday", "7": "Sunday",
						"一": "Monday", "二": "Tuesday", "三": "Wednesday", "四": "Thursday", "五": "Friday", "六": "Saturday", "日": "Sunday",
					}

					if val, ok := weekdayMap[input]; ok {
						recurrenceDetail = val
					} else {
						// 依然支援直接輸入英文全稱 (例如 Monday)
						validWeekdays := map[string]bool{"Sunday": true, "Monday": true, "Tuesday": true, "Wednesday": true, "Thursday": true, "Friday": true, "Saturday": true}
						if validWeekdays[input] {
							recurrenceDetail = input
						} else {
							fmt.Println("❌ 無效的輸入，請輸入 1-7、一-日 或英文全稱 (例如 Monday)")
							continue
						}
					}
				case "4":
					recurrenceType = "monthly"
					date = "" // 每月重複不需要特定日期
					fmt.Print("請輸入每月第幾天 (1-31，支援帶點號如 15.): ")
					if scanner.Scan() {
						recurrenceDetail = strings.TrimRight(strings.TrimSpace(scanner.Text()), ".")
					}
					day, err := strconv.Atoi(recurrenceDetail)
					if err != nil || day < 1 || day > 31 {
						fmt.Println("❌ 無效的日期，請輸入 1-31 的數字")
						continue
					}
				default:
					fmt.Println("❌ 無效的重複類型選擇")
					continue
				}
			} else {
				continue // 如果沒有輸入重複類型，則重新顯示選單
			}

			fmt.Print("請輸入時間 (例如 14:30): ")
			var t string
			if scanner.Scan() {
				t = strings.TrimSpace(scanner.Text())
			}

			// 簡易時間格式校驗
			if _, err := time.Parse("15:04", t); err != nil {
				fmt.Println("❌ 時間格式錯誤，請使用 HH:MM (例如 09:00)")
				continue
			}

			tm.AddTask(topic, content, date, t, recurrenceType, recurrenceDetail)
			fmt.Println("✅ 任務已成功排程！")
		case "2":
			fmt.Println("\n--- 當前任務清單 ---")
			tm.mu.Lock()
			// 排序邏輯：優先比較日期，再比較時間
			slices.SortFunc(tm.Tasks, func(a, b Task) int {
				// 優先處理一次性任務，然後是每日、每週、每月
				recurrenceOrder := map[string]int{"once": 0, "daily": 1, "weekly": 2, "monthly": 3}
				if recurrenceOrder[a.RecurrenceType] != recurrenceOrder[b.RecurrenceType] {
					return recurrenceOrder[a.RecurrenceType] - recurrenceOrder[b.RecurrenceType]
				}

				// 如果都是一次性任務，則比較日期
				if a.RecurrenceType == "once" && a.Date != b.Date {
					return strings.Compare(a.Date, b.Date)
				}
				// 對於重複任務，比較 RecurrenceDetail (例如星期幾或每月幾號)
				if a.RecurrenceType != "once" && a.RecurrenceDetail != b.RecurrenceDetail {
					return strings.Compare(a.RecurrenceDetail, b.RecurrenceDetail)
				}
				// 最後比較時間
				return strings.Compare(a.Time, b.Time)
			})

			if len(tm.Tasks) == 0 {
				fmt.Println("（目前沒有任何任務）")
			}

			// 顏色清單: 綠, 黃, 藍, 紫, 青
			colors := []string{"\033[32m", "\033[33m", "\033[34m", "\033[35m", "\033[36m"}
			colorMap := make(map[string]string)
			colorIdx := 0

			for _, task := range tm.Tasks {
				// 根據日期分配顏色
				displayColor, ok := colorMap[task.Date]
				if !ok {
					displayColor = colors[colorIdx%len(colors)]
					colorMap[task.Date] = displayColor
					colorIdx++
				}

				status := "⏳ 待處理"
				today := time.Now().Format("2006-01-02")
				if task.LastNotifiedDate == today {
					status = "✅ 已提醒"
				}

				recurrenceDisplay := ""
				switch task.RecurrenceType {
				case "once":
					recurrenceDisplay = task.Date
				case "daily":
					recurrenceDisplay = "每天"
				case "weekly":
					recurrenceDisplay = "每週 " + task.RecurrenceDetail
				case "monthly":
					recurrenceDisplay = "每月 " + task.RecurrenceDetail + " 號"
				}

				fmt.Printf("%sID:%d [%s %s] 主題:%-10s 內容:%-20s 備註:%-15s - %s\033[0m\n",
					displayColor,
					task.ID,
					recurrenceDisplay,
					task.Time,
					task.Topic,
					task.Content,
					task.Notes,
					status,
				)
			}
			tm.mu.Unlock()
		case "3":
			fmt.Println("\n--- 本月任務清單 (未來 30 天) ---")
			tm.mu.Lock()
			// 顯示前先進行排序
			slices.SortFunc(tm.Tasks, func(a, b Task) int {
				// 優先處理一次性任務，然後是每日、每週、每月
				recurrenceOrder := map[string]int{"once": 0, "daily": 1, "weekly": 2, "monthly": 3}
				if recurrenceOrder[a.RecurrenceType] != recurrenceOrder[b.RecurrenceType] {
					return recurrenceOrder[a.RecurrenceType] - recurrenceOrder[b.RecurrenceType]
				}

				// 如果都是一次性任務，則比較日期
				if a.RecurrenceType == "once" && a.Date != b.Date {
					return strings.Compare(a.Date, b.Date)
				}
				// 對於重複任務，比較 RecurrenceDetail (例如星期幾或每月幾號)
				if a.RecurrenceType != "once" && a.RecurrenceDetail != b.RecurrenceDetail {
					return strings.Compare(a.RecurrenceDetail, b.RecurrenceDetail)
				}
				// 最後比較時間
				return strings.Compare(a.Time, b.Time)
			})

			today := time.Now().Format("2006-01-02")
			future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
			count := 0

			colors := []string{"\033[32m", "\033[33m", "\033[34m", "\033[35m", "\033[36m"}
			colorMap := make(map[string]string)
			colorIdx := 0

			for _, task := range tm.Tasks {
				// 顯示「每天提醒」或是「日期在今天與 30 天後之間」的任務
				isWithinRange := false
				if task.RecurrenceType == "once" {
					isWithinRange = task.Date >= today && task.Date <= future
				} else { // daily, weekly, monthly 這些重複任務，只要 RecurrenceType 不是 "once" 都應該顯示
					isWithinRange = true
				}

				if isWithinRange {
					displayColor, ok := colorMap[task.Date]
					if !ok {
						displayColor = colors[colorIdx%len(colors)]
						colorMap[task.Date] = displayColor
						colorIdx++
					}

					status := "⏳ 待處理"
					if task.LastNotifiedDate == today {
						status = "✅ 已提醒"
					}

					recurrenceDisplay := ""
					switch task.RecurrenceType {
					case "once":
						recurrenceDisplay = task.Date
					case "daily":
						recurrenceDisplay = "每天"
					case "weekly":
						recurrenceDisplay = "每週 " + task.RecurrenceDetail
					case "monthly":
						recurrenceDisplay = "每月 " + task.RecurrenceDetail + " 號"
					}

					fmt.Printf("%sID:%d [%s %s] 主題:%-10s 內容:%-20s 備註:%-15s - %s\033[0m\n",
						displayColor,
						task.ID,
						recurrenceDisplay,
						task.Time,
						task.Topic,
						task.Content,
						task.Notes,
						status,
					)
					count++
				}
			}
			if count == 0 {
				fmt.Println("（目前一個月內沒有任何任務）")
			}
			tm.mu.Unlock()

		case "4":
			fmt.Print("請輸入要刪除的任務 ID: ")
			var id int
			if scanner.Scan() {
				input := strings.TrimSpace(scanner.Text())
				var err error
				id, err = strconv.Atoi(input)
				if err != nil {
					fmt.Println("❌ 請輸入有效的數字 ID")
					continue
				}
			}
			if tm.DeleteTask(id) {
				fmt.Println("✅ 任務已成功刪除")
			} else {
				fmt.Println("❌ 找不到該 ID 的任務")
			}
		case "5":
			fmt.Println("\n--- 正在生成 AI 分析提示詞 (複製下方內容貼給 Gemini) ---")
			tm.mu.Lock()
			var sb strings.Builder
			sb.WriteString("### 個人任務分析請求 ###\n")
			sb.WriteString("你好 Gemini，我是你的使用者。請分析我目前的任務清單，並以專業顧問的角度提供：\n")
			sb.WriteString("1. 優先級排序建議 (高/中/低)\n")
			sb.WriteString("2. 行程衝突警告與時間管理技巧\n")
			sb.WriteString("3. 針對具體內容的執行建議\n\n")
			sb.WriteString("當前任務如下：\n")

			today := time.Now().Format("2006-01-02")
			for _, t := range tm.Tasks {
				recur := t.RecurrenceType
				if t.RecurrenceDetail != "" {
					recur += " (" + t.RecurrenceDetail + ")"
				}
				status := "⏳ 待處理"
				if t.IsPending {
					status = "⚠️ 等待確認中"
				} else if t.LastNotifiedDate == today {
					status = "✅ 今日已提醒"
				}
				sb.WriteString(fmt.Sprintf("- [%s] %s | 主題：%s | 內容：%s | 備註：%s | 時間：%s %s\n",
					status, recur, t.Topic, t.Content, t.Notes, t.Date, t.Time))
			}
			tm.mu.Unlock()
			if len(tm.Tasks) == 0 {
				fmt.Println("（目前清單是空的，無法生成分析）")
			} else {
				fmt.Println(sb.String())
				fmt.Println("--------------------------------------------------")
			}
		case "6":
			fmt.Println("工具關閉中...")
			return
		default:
			fmt.Println("⚠️ 無效的選擇，請重新輸入")
		}
	}
}
