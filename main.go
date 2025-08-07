package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"github.com/kbinani/screenshot"
	"google.golang.org/api/option"
)

type Config struct {
	NameReplaces map[string]string `json:"name_replaces"`
}

type RankingEntry struct {
	Rank string `json:"rank"`
	Name string `json:"name"`
	PT   string `json:"pt"`
}

type RankingResponse struct {
	Ranking []RankingEntry `json:"ranking"`
}

type TableData struct {
	Rank    string
	Name    string
	Points  string
	Diff1h  string
	Diff6h  string
	Diff12h string
	Diff24h string
}

type Screenshot struct {
	Index      string
	Region     image.Rectangle
	WebhookURL string
	BasePath   string
}

// Windows API constants for sleep prevention
const (
	ES_SYSTEM_REQUIRED  = 0x00000001
	ES_DISPLAY_REQUIRED = 0x00000002
	ES_CONTINUOUS       = 0x80000000
)

// NoSleep manager for preventing system sleep and screen off
type NoSleepManager struct {
	isActive      bool
	preventScreen bool
	kernel32      *syscall.LazyDLL
	setThreadExec *syscall.LazyProc
}

// NewNoSleepManager creates a new NoSleep manager
func NewNoSleepManager() *NoSleepManager {
	if runtime.GOOS != "windows" {
		return nil
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setThreadExec := kernel32.NewProc("SetThreadExecutionState")

	return &NoSleepManager{
		kernel32:      kernel32,
		setThreadExec: setThreadExec,
	}
}

// Start prevents system sleep and optionally screen off
func (ns *NoSleepManager) Start(preventScreenOff bool) error {
	if ns == nil || runtime.GOOS != "windows" {
		return fmt.Errorf("NoSleep is only supported on Windows")
	}

	if ns.isActive {
		return nil
	}

	flags := ES_CONTINUOUS | ES_SYSTEM_REQUIRED
	if preventScreenOff {
		flags |= ES_DISPLAY_REQUIRED
		ns.preventScreen = true
	}

	ret, _, err := ns.setThreadExec.Call(uintptr(flags))
	if ret == 0 {
		return fmt.Errorf("failed to set thread execution state: %v", err)
	}

	ns.isActive = true
	return nil
}

// Stop restores normal sleep behavior
func (ns *NoSleepManager) Stop() error {
	if ns == nil || !ns.isActive {
		return nil
	}

	ret, _, err := ns.setThreadExec.Call(uintptr(ES_CONTINUOUS))
	if ret == 0 {
		return fmt.Errorf("failed to restore thread execution state: %v", err)
	}

	ns.isActive = false
	ns.preventScreen = false
	return nil
}

// IsActive returns whether NoSleep is currently active
func (ns *NoSleepManager) IsActive() bool {
	return ns != nil && ns.isActive
}

// IsPreventing returns whether screen-off prevention is active
func (ns *NoSleepManager) IsPreventingScreen() bool {
	return ns != nil && ns.preventScreen
}

// Custom theme with Japanese font support
type customTheme struct {
	fontResource fyne.Resource
}

func (t *customTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return theme.DefaultTheme().Color(name, variant)
}

func (t *customTheme) Font(style fyne.TextStyle) fyne.Resource {
	if t.fontResource != nil {
		return t.fontResource
	}
	return theme.DefaultTheme().Font(style)
}

func (t *customTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *customTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}

func NewScreenshot(index string, x, y, width, height int, webhookURL string) *Screenshot {
	return &Screenshot{
		Index:      index,
		Region:     image.Rect(x, y, x+width, y+height),
		WebhookURL: webhookURL,
		BasePath:   fmt.Sprintf("res/%s", index),
	}
}

func loadConfig() (*Config, error) {
	configFile := "name-mapping.json"
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		// Create default config
		defaultConfig := &Config{
			NameReplaces: map[string]string{
				"old word": "new word",
			},
		}
		return defaultConfig, nil
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func captureScreenshot(region image.Rectangle, outputPath string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	img, err := screenshot.CaptureRect(region)
	if err != nil {
		return err
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return png.Encode(file, img)
}

func geminiExtractFromImage(ctx context.Context, client *genai.Client, imagePath string) (*RankingResponse, error) {
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}

	model := client.GenerativeModel("gemini-1.5-flash")

	prompt := `Extract ranking data from 1st to 11th place and output as JSON in the following format. Output must be JSON only:
{"ranking": [{"rank": "1", "name": "player_name", "pt": "points"}, ...]}`

	resp, err := model.GenerateContent(ctx,
		genai.ImageData("image/png", imageBytes),
		genai.Text(prompt),
	)
	if err != nil {
		return nil, err
	}

	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("no response from Gemini")
	}

	responseText := ""
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			responseText += string(txt)
		}
	}

	fmt.Printf("📥 Gemini response.text:\n%s\n", responseText)

	// JSON部分だけ抽出
	re := regexp.MustCompile(`\{[\s\S]+\}`)
	match := re.FindString(responseText)
	if match == "" {
		return nil, fmt.Errorf("JSON object not found in response")
	}

	var result RankingResponse
	if err := json.Unmarshal([]byte(match), &result); err != nil {
		return nil, fmt.Errorf("JSON parse error: %v", err)
	}

	return &result, nil
}

// OCR functionality is currently handled by Gemini AI
// Use another OCR library if needed

func processPointText(pt string) string {
	// Remove non-numeric characters while keeping commas
	re := regexp.MustCompile(`[^0-9,]`)
	pt = re.ReplaceAllString(pt, "")
	if pt == "" {
		pt = "0"
	}
	return pt
}

func sendDiscordWebhook(webhookURL, username, content, imagePath string) error {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	// Add content
	if err := w.WriteField("username", username); err != nil {
		return err
	}
	if err := w.WriteField("content", content); err != nil {
		return err
	}

	// Add image file
	if imagePath != "" {
		file, err := os.Open(imagePath)
		if err != nil {
			return err
		}
		defer file.Close()

		fw, err := w.CreateFormFile("file", filepath.Base(imagePath))
		if err != nil {
			return err
		}

		if _, err := io.Copy(fw, file); err != nil {
			return err
		}
	}

	w.Close()

	req, err := http.NewRequest("POST", webhookURL, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Discord webhook failed with status: %d", resp.StatusCode)
	}

	return nil
}

func (s *Screenshot) Process(ctx context.Context, genaiClient *genai.Client, config *Config, now time.Time, gui *GUI) error {
	fileName := now.Format("200601021504") + ".png"
	imagePath := filepath.Join(s.BasePath, "screenshot", fileName)

	fmt.Printf("Screenshot process %s\n", imagePath)

	// Capture screenshot
	if err := captureScreenshot(s.Region, imagePath); err != nil {
		return fmt.Errorf("failed to capture screenshot: %v", err)
	}

	var result []string
	hymh := now.Format("2006010215")

	if s.Index != "0" {
		// Load existing JSON data
		jsonPath := filepath.Join(s.BasePath, "json", "datas.json")
		datas := make(map[string][]RankingEntry)
		if data, err := os.ReadFile(jsonPath); err == nil {
			json.Unmarshal(data, &datas)
		}

		// Use Gemini AI for OCR processing
		if s.Index == "1" || s.Index == "2" || s.Index == "3" || s.Index == "4" {
			geminiResult, err := geminiExtractFromImage(ctx, genaiClient, imagePath)
			if err != nil {
				fmt.Printf("Gemini OCR failed: %v\n", err)
			} else if geminiResult != nil {
				// Clear current time slot data
				datas[hymh] = []RankingEntry{}

				for i, item := range geminiResult.Ranking {
					name := item.Name
					pt := item.PT

					// Name replacement
					if replacement, exists := config.NameReplaces[name]; exists {
						name = replacement
					}

					// Clean pt value
					cleanPt := processPointText(pt)

					// Add to datas
					datas[hymh] = append(datas[hymh], RankingEntry{
						Rank: strconv.Itoa(i + 1),
						Name: name,
						PT:   cleanPt,
					})

					// Calculate point differences for different time periods
					ptDiffs := s.calculatePointDifferences(datas, hymh, name, cleanPt, now)

					// Format result with point differences like Python version
					result = append(result, fmt.Sprintf("%d. %-20s %12s\n   1h:%12s 6h:%12s\n  12h:%12s 24h:%12s",
						i+1, name, cleanPt,
						formatPointDiff(ptDiffs["1h"]),
						formatPointDiff(ptDiffs["6h"]),
						formatPointDiff(ptDiffs["12h"]),
						formatPointDiff(ptDiffs["24h"])))
				}

				// Save JSON data
				if err := s.saveJSON(datas); err != nil {
					fmt.Printf("Failed to save JSON: %v\n", err)
				}

				// Save CSV data
				if err := s.saveCSV(datas); err != nil {
					fmt.Printf("Failed to save CSV: %v\n", err)
				}

				// Update GUI with latest data
				if gui != nil {
					gui.loadRegionData(s.Index)
				}
			}
		}
	}

	// Discord Webhookに送信
	if s.WebhookURL != "" {
		if err := sendDiscordWebhook(s.WebhookURL, hymh, strings.Join(result, "\n"), imagePath); err != nil {
			fmt.Printf("Discord webhook failed: %v\n", err)
		}
	}

	fmt.Println(strings.Join(result, "\n"))
	return nil
}

func (s *Screenshot) calculatePointDifferences(datas map[string][]RankingEntry, currentTime, name, currentPt string, now time.Time) map[string]int {
	ptDiffs := make(map[string]int)
	periods := map[string]int{
		"1h":  1,
		"6h":  6,
		"12h": 12,
		"24h": 24,
	}

	currentPtInt, _ := strconv.Atoi(strings.ReplaceAll(currentPt, ",", ""))

	for period, hours := range periods {
		pastTime := now.Add(time.Duration(-hours) * time.Hour)
		pastTimeKey := pastTime.Format("2006010215")

		if pastData, exists := datas[pastTimeKey]; exists {
			for _, entry := range pastData {
				if entry.Name == name {
					pastPtInt, _ := strconv.Atoi(strings.ReplaceAll(entry.PT, ",", ""))
					ptDiffs[period] = currentPtInt - pastPtInt
					break
				}
			}
		} else {
			ptDiffs[period] = 0
		}
	}

	return ptDiffs
}

func formatPointDiff(diff int) string {
	if diff == 0 {
		return "0"
	}
	// Format with commas for thousands separator
	if diff > 0 {
		return fmt.Sprintf("+%s", addCommas(diff))
	} else {
		return fmt.Sprintf("-%s", addCommas(-diff))
	}
}

func addCommas(n int) string {
	str := strconv.Itoa(n)
	if len(str) <= 3 {
		return str
	}

	var result string
	for i, digit := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result += ","
		}
		result += string(digit)
	}
	return result
}

func (s *Screenshot) saveJSON(datas map[string][]RankingEntry) error {
	// Ensure json directory exists
	jsonDir := filepath.Join(s.BasePath, "json")
	if err := os.MkdirAll(jsonDir, 0755); err != nil {
		return err
	}

	jsonPath := filepath.Join(jsonDir, "datas.json")
	jsonData, err := json.MarshalIndent(datas, "", "    ")
	if err != nil {
		return err
	}

	return os.WriteFile(jsonPath, jsonData, 0644)
}

func (s *Screenshot) saveCSV(datas map[string][]RankingEntry) error {
	// Ensure csv directory exists
	csvDir := filepath.Join(s.BasePath, "csv")
	if err := os.MkdirAll(csvDir, 0755); err != nil {
		return err
	}

	csvPath := filepath.Join(csvDir, "datas.csv")
	file, err := os.Create(csvPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header with extended time periods
	header := []string{"年月日時", "順位", "名前", "ポイント", 
		"1h", "3h", "6h", "9h", "12h", "15h", "18h", "21h", "24h", 
		"36h(1.5d)", "48h(2d)", "60h(2.5d)", "72h(3d)", "84h(3.5d)", "96h(4d)", 
		"108h(4.5d)", "120h(5d)", "132h(5.5d)", "144h(6d)", "156h(6.5d)", "168h(7d)", "180h(7.5d)"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Sort timestamps and write data
	timestamps := make([]string, 0, len(datas))
	for timestamp := range datas {
		timestamps = append(timestamps, timestamp)
	}

	// Simple sort (could use sort.Strings for better sorting)
	for i := 0; i < len(timestamps); i++ {
		for j := i + 1; j < len(timestamps); j++ {
			if timestamps[i] > timestamps[j] {
				timestamps[i], timestamps[j] = timestamps[j], timestamps[i]
			}
		}
	}

	for _, timestamp := range timestamps {
		entries := datas[timestamp]
		currentTime, _ := time.Parse("2006010215", timestamp)

		for _, entry := range entries {
			pt, _ := strconv.Atoi(strings.ReplaceAll(entry.PT, ",", ""))

			// Calculate point differences for extended time periods (to match header)
			timePeriods := []int{1, 3, 6, 9, 12, 15, 18, 21, 24, 36, 48, 60, 72, 84, 96, 108, 120, 132, 144, 156, 168, 180}
			ptDiffsExtended := make([]string, len(timePeriods))

			for i, hours := range timePeriods {
				pastTime := currentTime.Add(time.Duration(-hours) * time.Hour)
				pastTimeKey := pastTime.Format("2006010215")

				ptDiff := 0
				if pastData, exists := datas[pastTimeKey]; exists {
					for _, pastEntry := range pastData {
						if pastEntry.Name == entry.Name {
							pastPt, _ := strconv.Atoi(strings.ReplaceAll(pastEntry.PT, ",", ""))
							ptDiff = pt - pastPt
							break
						}
					}
				}
				if ptDiff == 0 {
					ptDiffsExtended[i] = "-"
				} else if ptDiff > 0 {
					ptDiffsExtended[i] = fmt.Sprintf("+%s", addCommas(ptDiff))
				} else {
					ptDiffsExtended[i] = addCommas(ptDiff)
				}
			}

			record := []string{
				timestamp,
				entry.Rank,
				entry.Name,
				entry.PT,
			}
			record = append(record, ptDiffsExtended...)

			if err := writer.Write(record); err != nil {
				return err
			}
		}
	}

	return nil
}

func isRegionEnabled(regionIndex int, gui *GUI) bool {
	if gui == nil {
		return true // Default to enabled if no GUI
	}

	switch regionIndex {
	case 1:
		return gui.region1EnableCheck.Checked
	case 2:
		return gui.region2EnableCheck.Checked
	case 3:
		return gui.region3EnableCheck.Checked
	case 4:
		return gui.region4EnableCheck.Checked
	case 5:
		return gui.region5EnableCheck.Checked
	case 6:
		return gui.region6EnableCheck.Checked
	default:
		return true // Region 0 or unknown regions are always enabled
	}
}

func worker(ctx context.Context, gui *GUI) error {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	if geminiAPIKey == "" {
		return fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	keyLen := len(geminiAPIKey)
	if keyLen > 10 {
		keyLen = 10
	}
	fmt.Printf("Worker loaded GEMINI_API_KEY: %s...\n", geminiAPIKey[:keyLen])

	// Initialize Gemini client
	client, err := genai.NewClient(ctx, option.WithAPIKey(geminiAPIKey))
	if err != nil {
		return fmt.Errorf("failed to create Gemini client: %v", err)
	}
	defer client.Close()

	// Load config
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	now := time.Now()
	fmt.Printf("worker %v\n", now)

	// Execute screenshot processing
	screenshots := make([]*Screenshot, 0, 7)

	// Load regions from environment variables
	for i := 0; i < 7; i++ {
		regionStr := os.Getenv(fmt.Sprintf("REGION_%d", i))
		if regionStr == "" {
			fmt.Printf("Region %d not set in environment\n", i)
			continue
		}

		// Check if region is enabled (skip check for region 0 - always enabled)
		if i > 0 && gui != nil {
			enabled := isRegionEnabled(i, gui)
			if !enabled {
				fmt.Printf("Region %d is disabled, skipping\n", i)
				continue
			}
		}

		fmt.Printf("Loading REGION_%d: %s\n", i, regionStr)

		x, y, width, height, err := parseRegion(regionStr)
		if err != nil {
			log.Printf("Invalid region %d: %v", i, err)
			continue
		}

		webhook := os.Getenv(fmt.Sprintf("DISCORD_WEBHOOK_%d", i))
		screenshots = append(screenshots, NewScreenshot(strconv.Itoa(i), x, y, width, height, webhook))
		fmt.Printf("Created screenshot %d: x=%d, y=%d, w=%d, h=%d\n", i, x, y, width, height)
	}

	for _, shot := range screenshots {
		if err := shot.Process(ctx, client, config, now, gui); err != nil {
			fmt.Printf("Error in shot%s: %v\n", shot.Index, err)
		}
	}

	return nil
}

func mainLoop(ctx context.Context, desiredMinutes []int) {
	for {
		now := time.Now()

		// Calculate next execution time
		var nextTimes []time.Time
		for _, m := range desiredMinutes {
			nextTime := now.Truncate(time.Hour).Add(time.Duration(m) * time.Minute)
			if nextTime.Before(now) || nextTime.Equal(now) {
				nextTime = nextTime.Add(time.Hour)
			}
			nextTimes = append(nextTimes, nextTime)
		}

		// Select the earliest next run time
		nextRunTime := nextTimes[0]
		for _, t := range nextTimes[1:] {
			if t.Before(nextRunTime) {
				nextRunTime = t
			}
		}

		waitTime := nextRunTime.Sub(now)
		fmt.Printf("⏳ Next run at: %v, waiting %.1f seconds\n", nextRunTime, waitTime.Seconds())

		time.Sleep(waitTime)

		if err := worker(ctx, nil); err != nil {
			log.Printf("Worker error: %v", err)
		}
	}
}

type GUI struct {
	app                fyne.App
	window             fyne.Window
	isRunning          bool
	ctx                context.Context
	cancel             context.CancelFunc
	statusBinding      binding.String
	logBinding         binding.String
	intervalEntry      *widget.Entry
	desiredMinuteEntry *widget.Entry
	geminiKeyEntry     *widget.Entry
	webhook0Entry      *widget.Entry
	webhook1Entry      *widget.Entry
	webhook2Entry      *widget.Entry
	webhook3Entry      *widget.Entry
	webhook4Entry      *widget.Entry
	webhook5Entry      *widget.Entry
	webhook6Entry      *widget.Entry
	region0Entry       *widget.Entry
	region1Entry       *widget.Entry
	region2Entry       *widget.Entry
	region3Entry       *widget.Entry
	region4Entry       *widget.Entry
	region5Entry       *widget.Entry
	region6Entry       *widget.Entry
	noSleepManager     *NoSleepManager
	regionTabs         *container.AppTabs
	regionDataBindings map[string]binding.String
	regionTables       map[string]*widget.Table
	region1EnableCheck *widget.Check
	region2EnableCheck *widget.Check
	region3EnableCheck *widget.Check
	region4EnableCheck *widget.Check
	region5EnableCheck *widget.Check
	region6EnableCheck *widget.Check
	region1NameEntry   *widget.Entry
	region2NameEntry   *widget.Entry
	region3NameEntry   *widget.Entry
	region4NameEntry   *widget.Entry
	region5NameEntry   *widget.Entry
	region6NameEntry   *widget.Entry
}

func getScreenDimensions() (int, int, int, int) {
	// Get the first display bounds (primary monitor)
	bounds := screenshot.GetDisplayBounds(0)
	return bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy()
}

func NewGUI() *GUI {
	myApp := app.New()
	myApp.SetIcon(nil)

	// Load Japanese font if available
	if fontResource, err := fyne.LoadResourceFromPath("NotoSansJP-Medium.ttf"); err == nil {
		myApp.Settings().SetTheme(&customTheme{fontResource: fontResource})
	}

	myWindow := myApp.NewWindow("UNI'S ON AIR Speed Tracker")
	myWindow.Resize(fyne.NewSize(1400, 600))

	statusBinding := binding.NewString()
	statusBinding.Set("Stopped")

	logBinding := binding.NewString()
	logBinding.Set("Application started\n")

	// Create data bindings for each region
	regionDataBindings := make(map[string]binding.String)
	for i := 1; i <= 6; i++ {
		regionKey := fmt.Sprintf("region_%d", i)
		binding := binding.NewString()
		binding.Set("No data available")
		regionDataBindings[regionKey] = binding
	}

	gui := &GUI{
		app:                myApp,
		window:             myWindow,
		statusBinding:      statusBinding,
		logBinding:         logBinding,
		regionDataBindings: regionDataBindings,
		regionTables:       make(map[string]*widget.Table),
		noSleepManager:     NewNoSleepManager(),
	}

	return gui
}

func (g *GUI) addLog(message string) {
	current, _ := g.logBinding.Get()
	timestamp := time.Now().Format("15:04:05")
	newMessage := fmt.Sprintf("[%s] %s\n", timestamp, message)
	g.logBinding.Set(current + newMessage)
}

func (g *GUI) getRegionName(regionIndex string) string {
	switch regionIndex {
	case "1":
		if g.region1NameEntry != nil && g.region1NameEntry.Text != "" {
			return g.region1NameEntry.Text
		}
		return "Region 1"
	case "2":
		if g.region2NameEntry != nil && g.region2NameEntry.Text != "" {
			return g.region2NameEntry.Text
		}
		return "Region 2"
	case "3":
		if g.region3NameEntry != nil && g.region3NameEntry.Text != "" {
			return g.region3NameEntry.Text
		}
		return "Region 3"
	case "4":
		if g.region4NameEntry != nil && g.region4NameEntry.Text != "" {
			return g.region4NameEntry.Text
		}
		return "Region 4"
	case "5":
		if g.region5NameEntry != nil && g.region5NameEntry.Text != "" {
			return g.region5NameEntry.Text
		}
		return "Region 5"
	case "6":
		if g.region6NameEntry != nil && g.region6NameEntry.Text != "" {
			return g.region6NameEntry.Text
		}
		return "Region 6"
	default:
		return fmt.Sprintf("Region %s", regionIndex)
	}
}

func (g *GUI) updateRegionTabNames() {
	if g.regionTabs == nil {
		return
	}

	// Update tab names for regions 1-4
	for i := 0; i < len(g.regionTabs.Items); i++ {
		regionIndex := strconv.Itoa(i + 1)
		newTabName := g.getRegionName(regionIndex)
		g.regionTabs.Items[i].Text = newTabName
	}

	// Refresh the tabs display
	g.regionTabs.Refresh()
}

func (g *GUI) loadRegionData(regionIndex string) {
	regionKey := fmt.Sprintf("region_%s", regionIndex)
	binding, exists := g.regionDataBindings[regionKey]
	if !exists {
		return
	}

	// Load data from JSON file
	jsonPath := filepath.Join("res", regionIndex, "json", "datas.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		binding.Set(fmt.Sprintf("No data|%s", time.Now().Format("2006/01/02 15:04")))
		if table, exists := g.regionTables[regionKey]; exists {
			table.Refresh()
		}
		return
	}

	var datas map[string][]RankingEntry
	if err := json.Unmarshal(data, &datas); err != nil {
		binding.Set(fmt.Sprintf("Error|%s", time.Now().Format("2006/01/02 15:04")))
		if table, exists := g.regionTables[regionKey]; exists {
			table.Refresh()
		}
		return
	}

	if len(datas) == 0 {
		binding.Set(fmt.Sprintf("No data|%s", time.Now().Format("2006/01/02 15:04")))
		if table, exists := g.regionTables[regionKey]; exists {
			table.Refresh()
		}
		return
	}

	// Get the latest timestamp
	var latestTime string
	for timestamp := range datas {
		if timestamp > latestTime {
			latestTime = timestamp
		}
	}

	ranking := datas[latestTime]
	if len(ranking) == 0 {
		binding.Set(fmt.Sprintf("No entries|%s", time.Now().Format("2006/01/02 15:04")))
		if table, exists := g.regionTables[regionKey]; exists {
			table.Refresh()
		}
		return
	}

	// Parse timestamp for display
	parsedTime, err := time.Parse("2006010215", latestTime)
	var timeDisplay string
	if err != nil {
		timeDisplay = latestTime
	} else {
		timeDisplay = parsedTime.Format("2006/01/02 15:04")
	}

	// Create table data
	var tableData []TableData
	maxDisplay := 50 // Show up to 50 players in table
	if len(ranking) < maxDisplay {
		maxDisplay = len(ranking)
	}

	for i := 0; i < maxDisplay; i++ {
		entry := ranking[i]

		// Calculate point differences for different time periods
		ptDiffs := g.calculatePointDifferences(datas, latestTime, entry.Name, entry.PT)

		tableData = append(tableData, TableData{
			Rank:    fmt.Sprintf("%d", i+1),
			Name:    entry.Name,
			Points:  entry.PT,
			Diff1h:  formatPointDiff(ptDiffs["1h"]),
			Diff6h:  formatPointDiff(ptDiffs["6h"]),
			Diff12h: formatPointDiff(ptDiffs["12h"]),
			Diff24h: formatPointDiff(ptDiffs["24h"]),
		})
	}

	// Store table data in JSON format
	jsonData, _ := json.Marshal(tableData)
	binding.Set(fmt.Sprintf("%s|%s", string(jsonData), timeDisplay))

	// Refresh table
	if table, exists := g.regionTables[regionKey]; exists {
		table.Refresh()
	}
}

func (g *GUI) refreshAllRegionData() {
	for i := 1; i <= 6; i++ {
		g.loadRegionData(strconv.Itoa(i))
	}
}

func (g *GUI) openConfigFile() {
	configPath := "name-mapping.json"

	// Create name-mapping.json if it doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config, err := loadConfig()
		if err != nil {
			g.addLog(fmt.Sprintf("Failed to create name-mapping.json: %v", err))
			return
		}

		data, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			g.addLog(fmt.Sprintf("Failed to marshal config: %v", err))
			return
		}

		if err := os.WriteFile(configPath, data, 0644); err != nil {
			g.addLog(fmt.Sprintf("Failed to write name-mapping.json: %v", err))
			return
		}
		g.addLog("Created name-mapping.json with default settings")
	}

	// Open the file with default system editor
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use cmd /c start to open with default application
		cmd = exec.Command("cmd", "/c", "start", "", configPath)
	case "darwin":
		cmd = exec.Command("open", configPath)
	case "linux":
		cmd = exec.Command("xdg-open", configPath)
	default:
		g.addLog("Unsupported operating system for opening files")
		return
	}

	if err := cmd.Start(); err != nil {
		g.addLog(fmt.Sprintf("Failed to open name-mapping.json: %v", err))
	} else {
		g.addLog("Opened name-mapping.json in default editor")
	}
}

func (g *GUI) openRegionFile(regionIndex, fileType, fileName string) {
	filePath := filepath.Join("res", regionIndex, fileType, fileName)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		g.addLog(fmt.Sprintf("File not found: %s", filePath))
		return
	}

	// Open the file with default system application
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use cmd /c start to open with default application
		cmd = exec.Command("cmd", "/c", "start", "", filePath)
	case "darwin":
		cmd = exec.Command("open", filePath)
	case "linux":
		cmd = exec.Command("xdg-open", filePath)
	default:
		g.addLog("Unsupported operating system for opening files")
		return
	}

	if err := cmd.Start(); err != nil {
		g.addLog(fmt.Sprintf("Failed to open %s: %v", filePath, err))
	} else {
		g.addLog(fmt.Sprintf("Opened %s in default editor", filePath))
	}
}

func (g *GUI) calculatePointDifferences(datas map[string][]RankingEntry, currentTime, name, currentPt string) map[string]int {
	ptDiffs := make(map[string]int)
	periods := map[string]int{
		"1h":  1,
		"6h":  6,
		"12h": 12,
		"24h": 24,
	}

	// Parse current time
	currentTimeObj, err := time.Parse("2006010215", currentTime)
	if err != nil {
		// If parsing fails, return zeros
		for period := range periods {
			ptDiffs[period] = 0
		}
		return ptDiffs
	}

	currentPtInt, _ := strconv.Atoi(strings.ReplaceAll(currentPt, ",", ""))

	for period, hours := range periods {
		pastTime := currentTimeObj.Add(time.Duration(-hours) * time.Hour)
		pastTimeKey := pastTime.Format("2006010215")

		if pastData, exists := datas[pastTimeKey]; exists {
			for _, entry := range pastData {
				if entry.Name == name {
					pastPtInt, _ := strconv.Atoi(strings.ReplaceAll(entry.PT, ",", ""))
					ptDiffs[period] = currentPtInt - pastPtInt
					break
				}
			}
		} else {
			ptDiffs[period] = 0
		}
	}

	return ptDiffs
}

func (g *GUI) createUI() {
	// ステータス表示
	statusLabel := widget.NewLabelWithData(g.statusBinding)
	statusLabel.TextStyle.Bold = true

	// Settings form
	g.desiredMinuteEntry = widget.NewEntry()
	g.desiredMinuteEntry.SetText("1,15,30")
	g.desiredMinuteEntry.SetPlaceHolder("e.g., 1,15,30,45")

	g.geminiKeyEntry = widget.NewPasswordEntry()
	g.webhook0Entry = widget.NewEntry()
	g.webhook1Entry = widget.NewEntry()
	g.webhook2Entry = widget.NewEntry()
	g.webhook3Entry = widget.NewEntry()
	g.webhook4Entry = widget.NewEntry()
	g.webhook5Entry = widget.NewEntry()
	g.webhook6Entry = widget.NewEntry()

	// Region entries (x,y,width,height)
	g.region0Entry = widget.NewEntry()
	// Auto-set region0 to full screen dimensions
	x, y, width, height := getScreenDimensions()
	g.region0Entry.SetText(fmt.Sprintf("%d,%d,%d,%d", x, y, width, height))
	g.region0Entry.SetPlaceHolder("Full screen (auto-detected)")
	g.region0Entry.Disable() // Make it read-only since it's auto-detected
	g.region1Entry = widget.NewEntry()
	g.region1Entry.SetText("191,0,535,722")
	g.region1Entry.SetPlaceHolder("x,y,width,height")
	g.region2Entry = widget.NewEntry()
	g.region2Entry.SetText("918,0,726,722")
	g.region2Entry.SetPlaceHolder("x,y,width,height")
	g.region3Entry = widget.NewEntry()
	g.region3Entry.SetText("1644,0,726,722")
	g.region3Entry.SetPlaceHolder("x,y,width,height")
	g.region4Entry = widget.NewEntry()
	g.region4Entry.SetText("191,722,726,722")
	g.region4Entry.SetPlaceHolder("x,y,width,height")
	g.region5Entry = widget.NewEntry()
	g.region5Entry.SetText("918,722,726,722")
	g.region5Entry.SetPlaceHolder("x,y,width,height")
	g.region6Entry = widget.NewEntry()
	g.region6Entry.SetText("1644,722,726,722")
	g.region6Entry.SetPlaceHolder("x,y,width,height")

	// Region enable/disable checkboxes
	g.region1EnableCheck = widget.NewCheck("有効", nil)
	g.region1EnableCheck.SetChecked(true) // Default enabled
	g.region2EnableCheck = widget.NewCheck("有効", nil)
	g.region2EnableCheck.SetChecked(true) // Default enabled
	g.region3EnableCheck = widget.NewCheck("有効", nil)
	g.region3EnableCheck.SetChecked(true) // Default enabled
	g.region4EnableCheck = widget.NewCheck("有効", nil)
	g.region4EnableCheck.SetChecked(true) // Default enabled
	g.region5EnableCheck = widget.NewCheck("有効", nil)
	g.region5EnableCheck.SetChecked(true) // Default enabled
	g.region6EnableCheck = widget.NewCheck("有効", nil)
	g.region6EnableCheck.SetChecked(true) // Default enabled

	// Region name entries
	g.region1NameEntry = widget.NewEntry()
	g.region1NameEntry.SetText("Region 1")
	g.region1NameEntry.SetPlaceHolder("Region name")
	g.region2NameEntry = widget.NewEntry()
	g.region2NameEntry.SetText("Region 2")
	g.region2NameEntry.SetPlaceHolder("Region name")
	g.region3NameEntry = widget.NewEntry()
	g.region3NameEntry.SetText("Region 3")
	g.region3NameEntry.SetPlaceHolder("Region name")
	g.region4NameEntry = widget.NewEntry()
	g.region4NameEntry.SetText("Region 4")
	g.region4NameEntry.SetPlaceHolder("Region name")
	g.region5NameEntry = widget.NewEntry()
	g.region5NameEntry.SetText("Region 5")
	g.region5NameEntry.SetPlaceHolder("Region name")
	g.region6NameEntry = widget.NewEntry()
	g.region6NameEntry.SetText("Region 6")
	g.region6NameEntry.SetPlaceHolder("Region name")

	// Load settings from .env file
	g.loadFromEnvFile()

	// Create region selection buttons
	// Region0 is full screen - add refresh button to re-detect screen size
	refreshBtn := widget.NewButton("更新", func() {
		x, y, width, height := getScreenDimensions()
		g.region0Entry.Enable()
		g.region0Entry.SetText(fmt.Sprintf("%d,%d,%d,%d", x, y, width, height))
		g.region0Entry.Disable()
		g.addLog("Screen dimensions refreshed")
	})
	region0Container := container.NewBorder(nil, nil, nil, refreshBtn, container.NewMax(g.region0Entry))
	region1Container := container.NewGridWithColumns(4,
		g.region1EnableCheck,
		g.region1NameEntry,
		g.region1Entry,
		widget.NewButton("選択", func() { g.showRegionSelector(g.region1Entry) }))
	region2Container := container.NewGridWithColumns(4,
		g.region2EnableCheck,
		g.region2NameEntry,
		g.region2Entry,
		widget.NewButton("選択", func() { g.showRegionSelector(g.region2Entry) }))
	region3Container := container.NewGridWithColumns(4,
		g.region3EnableCheck,
		g.region3NameEntry,
		g.region3Entry,
		widget.NewButton("選択", func() { g.showRegionSelector(g.region3Entry) }))
	region4Container := container.NewGridWithColumns(4,
		g.region4EnableCheck,
		g.region4NameEntry,
		g.region4Entry,
		widget.NewButton("選択", func() { g.showRegionSelector(g.region4Entry) }))
	region5Container := container.NewGridWithColumns(4,
		g.region5EnableCheck,
		g.region5NameEntry,
		g.region5Entry,
		widget.NewButton("選択", func() { g.showRegionSelector(g.region5Entry) }))
	region6Container := container.NewGridWithColumns(4,
		g.region6EnableCheck,
		g.region6NameEntry,
		g.region6Entry,
		widget.NewButton("選択", func() { g.showRegionSelector(g.region6Entry) }))

	settingsForm := container.NewVBox(
		widget.NewLabel("Settings"),
		widget.NewForm(
			widget.NewFormItem("Execution times (minutes)", g.desiredMinuteEntry),
			widget.NewFormItem("Gemini API Key", g.geminiKeyEntry),
			widget.NewFormItem("Discord Webhook 0", g.webhook0Entry),
			widget.NewFormItem("Discord Webhook 1", g.webhook1Entry),
			widget.NewFormItem("Discord Webhook 2", g.webhook2Entry),
			widget.NewFormItem("Discord Webhook 3", g.webhook3Entry),
			widget.NewFormItem("Discord Webhook 4", g.webhook4Entry),
			widget.NewFormItem("Discord Webhook 5", g.webhook5Entry),
			widget.NewFormItem("Discord Webhook 6", g.webhook6Entry),
			widget.NewFormItem("Region 0 (Full Screen)", region0Container),
			widget.NewFormItem("Region 1 (x,y,w,h)", region1Container),
			widget.NewFormItem("Region 2 (x,y,w,h)", region2Container),
			widget.NewFormItem("Region 3 (x,y,w,h)", region3Container),
			widget.NewFormItem("Region 4 (x,y,w,h)", region4Container),
			widget.NewFormItem("Region 5 (x,y,w,h)", region5Container),
			widget.NewFormItem("Region 6 (x,y,w,h)", region6Container),
		),
	)

	// Control buttons
	startButton := widget.NewButton("開始", g.startScreenshot)
	stopButton := widget.NewButton("停止", g.stopScreenshot)
	stopButton.Disable()

	saveButton := widget.NewButton("設定保存", func() {
		if err := g.saveToEnvFile(); err != nil {
			g.addLog(fmt.Sprintf("Failed to save settings: %v", err))
		} else {
			g.addLog("Settings saved to .env file")
			// Update tab names to reflect any changes
			g.updateRegionTabNames()
		}
	})

	configButton := widget.NewButton("name-mapping.json を開く", func() {
		g.openConfigFile()
	})

	controlsContainer := container.NewHBox(
		startButton,
		stopButton,
		saveButton,
		configButton,
	)

	// Log display
	logLabel := widget.NewRichTextFromMarkdown("")
	logLabel.Wrapping = fyne.TextWrapWord
	logScroll := container.NewScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(400, 160))

	// Monitor log updates
	g.logBinding.AddListener(binding.NewDataListener(func() {
		current, _ := g.logBinding.Get()
		logLabel.ParseMarkdown(fmt.Sprintf("```\n%s\n```", current))
		// Auto scroll
		logScroll.ScrollToBottom()
	}))

	// Create tabs for regions
	g.regionTabs = container.NewAppTabs()

	// Create tab content for each region
	for i := 1; i <= 6; i++ {
		regionIndex := strconv.Itoa(i)
		regionKey := fmt.Sprintf("region_%s", regionIndex)

		// Create update time label
		updateTimeLabel := widget.NewLabel("最終更新: -")
		updateTimeLabel.TextStyle = fyne.TextStyle{Italic: true}

		// Create table for this region
		var tableData []TableData
		regionTable := widget.NewTable(
			func() (int, int) {
				return len(tableData) + 1, 7 // +1 for header, 7 columns
			},
			func() fyne.CanvasObject {
				label := widget.NewLabel("")
				label.Alignment = fyne.TextAlignCenter
				return label
			},
			func(i widget.TableCellID, o fyne.CanvasObject) {
				label := o.(*widget.Label)

				// Header row
				if i.Row == 0 {
					label.TextStyle = fyne.TextStyle{Bold: true}
					switch i.Col {
					case 0:
						label.SetText("順位")
						label.Alignment = fyne.TextAlignCenter
					case 1:
						label.SetText("プレイヤー名")
						label.Alignment = fyne.TextAlignLeading
					case 2:
						label.SetText("ポイント")
						label.Alignment = fyne.TextAlignTrailing
					case 3:
						label.SetText("1h差")
						label.Alignment = fyne.TextAlignTrailing
					case 4:
						label.SetText("6h差")
						label.Alignment = fyne.TextAlignTrailing
					case 5:
						label.SetText("12h差")
						label.Alignment = fyne.TextAlignTrailing
					case 6:
						label.SetText("24h差")
						label.Alignment = fyne.TextAlignTrailing
					}
					return
				}

				// Data rows
				if i.Row-1 < len(tableData) {
					data := tableData[i.Row-1]
					label.TextStyle = fyne.TextStyle{Bold: false}

					switch i.Col {
					case 0:
						label.SetText(data.Rank)
						label.Alignment = fyne.TextAlignCenter
						// Gold/Silver/Bronze colors for top 3
						rank, _ := strconv.Atoi(data.Rank)
						if rank == 1 {
							label.TextStyle = fyne.TextStyle{Bold: true}
						}
					case 1:
						label.SetText(data.Name)
						label.Alignment = fyne.TextAlignLeading
					case 2:
						label.SetText(data.Points)
						label.Alignment = fyne.TextAlignTrailing
					case 3:
						label.SetText(data.Diff1h)
						label.Alignment = fyne.TextAlignTrailing
						if strings.HasPrefix(data.Diff1h, "+") {
							label.TextStyle = fyne.TextStyle{Bold: true}
						}
					case 4:
						label.SetText(data.Diff6h)
						label.Alignment = fyne.TextAlignTrailing
						if strings.HasPrefix(data.Diff6h, "+") {
							label.TextStyle = fyne.TextStyle{Bold: true}
						}
					case 5:
						label.SetText(data.Diff12h)
						label.Alignment = fyne.TextAlignTrailing
						if strings.HasPrefix(data.Diff12h, "+") {
							label.TextStyle = fyne.TextStyle{Bold: true}
						}
					case 6:
						label.SetText(data.Diff24h)
						label.Alignment = fyne.TextAlignTrailing
						if strings.HasPrefix(data.Diff24h, "+") {
							label.TextStyle = fyne.TextStyle{Bold: true}
						}
					}
				}
			},
		)

		// Set column widths
		regionTable.SetColumnWidth(0, 60)  // Rank
		regionTable.SetColumnWidth(1, 180) // Name
		regionTable.SetColumnWidth(2, 100) // Points
		regionTable.SetColumnWidth(3, 80)  // 1h
		regionTable.SetColumnWidth(4, 80)  // 6h
		regionTable.SetColumnWidth(5, 80)  // 12h
		regionTable.SetColumnWidth(6, 80)  // 24h

		// Store table reference
		g.regionTables[regionKey] = regionTable

		// Monitor data updates for this region
		localRegionIndex := regionIndex
		localRegionKey := regionKey
		localTable := regionTable
		localUpdateLabel := updateTimeLabel

		g.regionDataBindings[localRegionKey].AddListener(binding.NewDataListener(func() {
			current, _ := g.regionDataBindings[localRegionKey].Get()
			parts := strings.Split(current, "|")

			if len(parts) == 2 {
				// Parse JSON data
				var newData []TableData
				if err := json.Unmarshal([]byte(parts[0]), &newData); err == nil {
					tableData = newData
					localTable.Refresh()
				}
				// Update time label
				localUpdateLabel.SetText(fmt.Sprintf("最終更新: %s", parts[1]))
			} else {
				// Handle error messages
				tableData = nil
				localTable.Refresh()
				localUpdateLabel.SetText("最終更新: -")
			}
		}))

		// Add buttons for each tab
		refreshBtn := widget.NewButton("更新", func() {
			g.loadRegionData(localRegionIndex)
		})

		csvBtn := widget.NewButton("CSV を開く", func() {
			g.openRegionFile(localRegionIndex, "csv", "datas.csv")
		})

		jsonBtn := widget.NewButton("JSON を開く", func() {
			g.openRegionFile(localRegionIndex, "json", "datas.json")
		})

		tableScroll := container.NewScroll(regionTable)
		tableScroll.SetMinSize(fyne.NewSize(700, 480))

		tabContent := container.NewVBox(
			container.NewHBox(refreshBtn, csvBtn, jsonBtn, widget.NewSeparator(), updateTimeLabel),
			tableScroll,
		)

		tabItem := container.NewTabItem(g.getRegionName(localRegionIndex), tabContent)
		g.regionTabs.Append(tabItem)
	}

	// Load initial data for all regions
	g.refreshAllRegionData()

	// Layout
	leftPanel := container.NewVBox(
		widget.NewLabel("Status"),
		statusLabel,
		widget.NewSeparator(),
		settingsForm,
		widget.NewSeparator(),
		controlsContainer,
	)

	// Create header with label and button
	rankingsHeader := container.NewBorder(
		nil, nil,
		widget.NewLabel("Region Rankings"),
		widget.NewButton("ビューアーを開く", func() {
			g.openWebViewer()
		}),
		nil,
	)

	rightPanel := container.NewVBox(
		widget.NewLabel("Log"),
		logScroll,
		widget.NewSeparator(),
		rankingsHeader,
		g.regionTabs,
	)

	content := container.NewHSplit(leftPanel, rightPanel)
	content.SetOffset(0.5) // Set left panel to 50%

	g.window.SetContent(content)

	// Manage start/stop button states
	g.statusBinding.AddListener(binding.NewDataListener(func() {
		status, _ := g.statusBinding.Get()
		if strings.Contains(status, "Running") {
			startButton.Disable()
			stopButton.Enable()
		} else {
			startButton.Enable()
			stopButton.Disable()
		}
	}))
}

func (g *GUI) startScreenshot() {
	if g.isRunning {
		return
	}

	// Validate settings (use current GUI values)
	if err := g.validateSettings(); err != nil {
		dialog.ShowError(err, g.window)
		return
	}

	g.isRunning = true
	g.ctx, g.cancel = context.WithCancel(context.Background())

	desiredMinutes, _ := parseDesiredMinutes(g.desiredMinuteEntry.Text)

	g.statusBinding.Set(fmt.Sprintf("Running (at minutes: %v)", desiredMinutes))
	g.addLog("Screenshot process started")

	// Start sleep prevention (always enabled with screen off prevention)
	if err := g.noSleepManager.Start(true); err != nil {
		g.addLog(fmt.Sprintf("Warning: Failed to enable sleep prevention: %v", err))
	} else {
		g.addLog("Sleep prevention enabled (including screen off)")
	}

	// Update environment variables with current GUI values
	g.updateEnvironmentVariables()

	// Save current GUI settings to .env file
	if err := g.saveToEnvFile(); err != nil {
		g.addLog(fmt.Sprintf("Warning: Failed to save settings: %v", err))
	} else {
		g.addLog("Current settings saved to .env file")
	}

	// Run in background
	go g.runMainLoop(desiredMinutes)
}

func (g *GUI) stopScreenshot() {
	if !g.isRunning {
		return
	}

	g.isRunning = false
	if g.cancel != nil {
		g.cancel()
	}

	// Stop sleep prevention
	if g.noSleepManager.IsActive() {
		if err := g.noSleepManager.Stop(); err != nil {
			g.addLog(fmt.Sprintf("Warning: Failed to disable sleep prevention: %v", err))
		} else {
			g.addLog("Sleep prevention disabled")
		}
	}

	g.statusBinding.Set("Stopped")
	g.addLog("Screenshot process stopped")
}

func parseDesiredMinutes(input string) ([]int, error) {
	parts := strings.Split(input, ",")
	minutes := make([]int, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		minute, err := strconv.Atoi(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid minute value: %s", trimmed)
		}

		if minute < 0 || minute > 59 {
			return nil, fmt.Errorf("minute must be between 0 and 59: %d", minute)
		}

		minutes = append(minutes, minute)
	}

	if len(minutes) == 0 {
		return nil, fmt.Errorf("at least one minute must be specified")
	}

	return minutes, nil
}

func parseRegion(input string) (x, y, width, height int, err error) {
	if input == "" {
		return 0, 0, 0, 0, fmt.Errorf("region cannot be empty")
	}

	parts := strings.Split(input, ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("region must have 4 values: x,y,width,height")
	}

	values := make([]int, 4)
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		val, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("invalid number at position %d: %s", i+1, trimmed)
		}
		values[i] = val
	}

	return values[0], values[1], values[2], values[3], nil
}

func (g *GUI) validateSettings() error {
	if g.geminiKeyEntry.Text == "" {
		return fmt.Errorf("Please enter Gemini API Key")
	}

	if _, err := parseDesiredMinutes(g.desiredMinuteEntry.Text); err != nil {
		return fmt.Errorf("Invalid execution times: %v", err)
	}

	return nil
}

func (g *GUI) updateEnvironmentVariables() {
	os.Setenv("GEMINI_API_KEY", g.geminiKeyEntry.Text)
	os.Setenv("DISCORD_WEBHOOK_0", g.webhook0Entry.Text)
	os.Setenv("DISCORD_WEBHOOK_1", g.webhook1Entry.Text)
	os.Setenv("DISCORD_WEBHOOK_2", g.webhook2Entry.Text)
	os.Setenv("DISCORD_WEBHOOK_3", g.webhook3Entry.Text)
	os.Setenv("DISCORD_WEBHOOK_4", g.webhook4Entry.Text)
	os.Setenv("DISCORD_WEBHOOK_5", g.webhook5Entry.Text)
	os.Setenv("DISCORD_WEBHOOK_6", g.webhook6Entry.Text)
	os.Setenv("REGION_0", g.region0Entry.Text)
	os.Setenv("REGION_1", g.region1Entry.Text)
	os.Setenv("REGION_2", g.region2Entry.Text)
	os.Setenv("REGION_3", g.region3Entry.Text)
	os.Setenv("REGION_4", g.region4Entry.Text)
	os.Setenv("REGION_5", g.region5Entry.Text)
	os.Setenv("REGION_6", g.region6Entry.Text)
}

func (g *GUI) saveToEnvFile() error {
	content := fmt.Sprintf(`GEMINI_API_KEY=%s
DISCORD_WEBHOOK_0=%s
DISCORD_WEBHOOK_1=%s
DISCORD_WEBHOOK_2=%s
DISCORD_WEBHOOK_3=%s
DISCORD_WEBHOOK_4=%s
DISCORD_WEBHOOK_5=%s
DISCORD_WEBHOOK_6=%s
DESIRED_MINUTES=%s
REGION_0=%s
REGION_1=%s
REGION_2=%s
REGION_3=%s
REGION_4=%s
REGION_5=%s
REGION_6=%s
REGION_1_ENABLED=%t
REGION_2_ENABLED=%t
REGION_3_ENABLED=%t
REGION_4_ENABLED=%t
REGION_5_ENABLED=%t
REGION_6_ENABLED=%t
REGION_1_NAME=%s
REGION_2_NAME=%s
REGION_3_NAME=%s
REGION_4_NAME=%s
REGION_5_NAME=%s
REGION_6_NAME=%s
`, g.geminiKeyEntry.Text, g.webhook0Entry.Text, g.webhook1Entry.Text, g.webhook2Entry.Text, g.webhook3Entry.Text, g.webhook4Entry.Text, g.webhook5Entry.Text, g.webhook6Entry.Text, g.desiredMinuteEntry.Text, g.region0Entry.Text, g.region1Entry.Text, g.region2Entry.Text, g.region3Entry.Text, g.region4Entry.Text, g.region5Entry.Text, g.region6Entry.Text, g.region1EnableCheck.Checked, g.region2EnableCheck.Checked, g.region3EnableCheck.Checked, g.region4EnableCheck.Checked, g.region5EnableCheck.Checked, g.region6EnableCheck.Checked, g.region1NameEntry.Text, g.region2NameEntry.Text, g.region3NameEntry.Text, g.region4NameEntry.Text, g.region5NameEntry.Text, g.region6NameEntry.Text)

	return os.WriteFile(".env", []byte(content), 0644)
}

func (g *GUI) loadFromEnvFile() {
	// Load .env file if it exists
	if err := godotenv.Load(); err == nil {
		// Update GUI fields with loaded values
		if val := os.Getenv("GEMINI_API_KEY"); val != "" {
			g.geminiKeyEntry.SetText(val)
		}
		if val := os.Getenv("DISCORD_WEBHOOK_0"); val != "" {
			g.webhook0Entry.SetText(val)
		}
		if val := os.Getenv("DISCORD_WEBHOOK_1"); val != "" {
			g.webhook1Entry.SetText(val)
		}
		if val := os.Getenv("DISCORD_WEBHOOK_2"); val != "" {
			g.webhook2Entry.SetText(val)
		}
		if val := os.Getenv("DISCORD_WEBHOOK_3"); val != "" {
			g.webhook3Entry.SetText(val)
		}
		if val := os.Getenv("DISCORD_WEBHOOK_4"); val != "" {
			g.webhook4Entry.SetText(val)
		}
		if val := os.Getenv("DISCORD_WEBHOOK_5"); val != "" {
			g.webhook5Entry.SetText(val)
		}
		if val := os.Getenv("DISCORD_WEBHOOK_6"); val != "" {
			g.webhook6Entry.SetText(val)
		}
		if val := os.Getenv("DESIRED_MINUTES"); val != "" {
			g.desiredMinuteEntry.SetText(val)
		}
		// Region 0 is auto-detected screen size, only override if explicitly set in .env
		if val := os.Getenv("REGION_0"); val != "" && val != "auto" {
			g.region0Entry.Enable()
			g.region0Entry.SetText(val)
			g.region0Entry.Disable()
		}
		if val := os.Getenv("REGION_1"); val != "" {
			g.region1Entry.SetText(val)
		}
		if val := os.Getenv("REGION_2"); val != "" {
			g.region2Entry.SetText(val)
		}
		if val := os.Getenv("REGION_3"); val != "" {
			g.region3Entry.SetText(val)
		}
		if val := os.Getenv("REGION_4"); val != "" {
			g.region4Entry.SetText(val)
		}
		if val := os.Getenv("REGION_5"); val != "" {
			g.region5Entry.SetText(val)
		}
		if val := os.Getenv("REGION_6"); val != "" {
			g.region6Entry.SetText(val)
		}
		// Load region enabled states
		if val := os.Getenv("REGION_1_ENABLED"); val != "" {
			g.region1EnableCheck.SetChecked(val == "true")
		}
		if val := os.Getenv("REGION_2_ENABLED"); val != "" {
			g.region2EnableCheck.SetChecked(val == "true")
		}
		if val := os.Getenv("REGION_3_ENABLED"); val != "" {
			g.region3EnableCheck.SetChecked(val == "true")
		}
		if val := os.Getenv("REGION_4_ENABLED"); val != "" {
			g.region4EnableCheck.SetChecked(val == "true")
		}
		if val := os.Getenv("REGION_5_ENABLED"); val != "" {
			g.region5EnableCheck.SetChecked(val == "true")
		}
		if val := os.Getenv("REGION_6_ENABLED"); val != "" {
			g.region6EnableCheck.SetChecked(val == "true")
		}
		// Load region names
		if val := os.Getenv("REGION_1_NAME"); val != "" {
			g.region1NameEntry.SetText(val)
		}
		if val := os.Getenv("REGION_2_NAME"); val != "" {
			g.region2NameEntry.SetText(val)
		}
		if val := os.Getenv("REGION_3_NAME"); val != "" {
			g.region3NameEntry.SetText(val)
		}
		if val := os.Getenv("REGION_4_NAME"); val != "" {
			g.region4NameEntry.SetText(val)
		}
		if val := os.Getenv("REGION_5_NAME"); val != "" {
			g.region5NameEntry.SetText(val)
		}
		if val := os.Getenv("REGION_6_NAME"); val != "" {
			g.region6NameEntry.SetText(val)
		}
	}
}

func (g *GUI) runMainLoop(desiredMinutes []int) {
	for {
		now := time.Now()

		// Calculate next execution time
		var nextTimes []time.Time
		for _, m := range desiredMinutes {
			nextTime := now.Truncate(time.Hour).Add(time.Duration(m) * time.Minute)
			if nextTime.Before(now) || nextTime.Equal(now) {
				nextTime = nextTime.Add(time.Hour)
			}
			nextTimes = append(nextTimes, nextTime)
		}

		// Select the earliest next run time
		nextRunTime := nextTimes[0]
		for _, t := range nextTimes[1:] {
			if t.Before(nextRunTime) {
				nextRunTime = t
			}
		}

		waitTime := nextRunTime.Sub(now)
		g.addLog(fmt.Sprintf("Next run at: %v, waiting %.1f seconds", nextRunTime.Format("15:04:05"), waitTime.Seconds()))

		// Wait until next run time or context cancellation
		select {
		case <-g.ctx.Done():
			return
		case <-time.After(waitTime):
			g.addLog("Running screenshot process...")
			if err := worker(g.ctx, g); err != nil {
				g.addLog(fmt.Sprintf("Error occurred: %v", err))
			} else {
				g.addLog("Screenshot process completed")
			}
		}
	}
}

func (g *GUI) Run() {
	g.createUI()
	g.window.ShowAndRun()
}

// showRegionSelector shows a screenshot with region selection
func (g *GUI) showRegionSelector(targetEntry *widget.Entry) {
	// Hide main window temporarily
	g.window.Hide()

	// Wait a bit for window to hide
	time.Sleep(200 * time.Millisecond)

	// Capture full screen
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		g.addLog(fmt.Sprintf("Failed to capture screen: %v", err))
		g.window.Show()
		return
	}

	// Create selection window
	selectWindow := g.app.NewWindow("Select Region - Click and drag to select")
	selectWindow.Resize(fyne.NewSize(float32(bounds.Dx())/2, float32(bounds.Dy())/2))
	selectWindow.CenterOnScreen()

	// Convert image to resource
	fyneImage := canvas.NewImageFromImage(img)
	fyneImage.FillMode = canvas.ImageFillContain

	// Variables for selection
	var startX, startY, endX, endY float32
	var selecting bool
	var selectionRect *canvas.Rectangle

	// Create selection rectangle
	selectionRect = canvas.NewRectangle(color.Transparent)
	selectionRect.StrokeColor = color.RGBA{255, 0, 0, 255}
	selectionRect.StrokeWidth = 2
	selectionRect.FillColor = color.Transparent
	selectionRect.Hide() // Initially hidden

	// Create image container with selection overlay
	imageWithSelection := container.NewWithoutLayout(fyneImage, selectionRect)
	scroll := container.NewScroll(imageWithSelection)

	// Set up keyboard handling
	selectWindow.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape {
			selectWindow.Close()
			g.window.Show()
		}
	})

	// Coordinate display
	coordLabel := widget.NewLabel("Drag to select region, then click Confirm")

	// Buttons
	confirmBtn := widget.NewButton("Confirm", func() {
		if selecting && abs(endX-startX) > 5 && abs(endY-startY) > 5 {
			// Use the same calculation as onSelectionUpdate for consistency
			imageDisplaySize := fyneImage.Size()
			screenWidth := float32(bounds.Dx())
			screenHeight := float32(bounds.Dy())

			// Calculate scale factor (ImageFillContain scales to fit inside while preserving aspect ratio)
			scaleX := imageDisplaySize.Width / screenWidth
			scaleY := imageDisplaySize.Height / screenHeight
			scale := min(scaleX, scaleY) // Use smaller scale for ImageFillContain

			// Calculate the actual displayed image size
			actualImageWidth := screenWidth * scale
			actualImageHeight := screenHeight * scale

			// Calculate letterbox offsets (centering)
			offsetX := (imageDisplaySize.Width - actualImageWidth) / 2
			offsetY := (imageDisplaySize.Height - actualImageHeight) / 2

			// Adjust coordinates for letterboxing
			adjustedStartX := startX - offsetX
			adjustedStartY := startY - offsetY
			adjustedEndX := endX - offsetX
			adjustedEndY := endY - offsetY

			// Convert to screen coordinates
			x := int(min(adjustedStartX, adjustedEndX) / scale)
			y := int(min(adjustedStartY, adjustedEndY) / scale)
			width := int(abs(adjustedEndX-adjustedStartX) / scale)
			height := int(abs(adjustedEndY-adjustedStartY) / scale)

			// Ensure minimum size
			if width < 10 {
				width = 10
			}
			if height < 10 {
				height = 10
			}

			targetEntry.SetText(fmt.Sprintf("%d,%d,%d,%d", x, y, width, height))
			g.addLog(fmt.Sprintf("Selected region: x=%d, y=%d, width=%d, height=%d", x, y, width, height))

			selectWindow.Close()
			g.window.Show()
		} else {
			coordLabel.SetText("Please drag to select a larger region (minimum 5x5 pixels)")
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		selectWindow.Close()
		g.window.Show()
	})

	instructionLabel := widget.NewLabel("Instructions: Click and drag on the image to select a region")

	bottom := container.NewVBox(
		instructionLabel,
		coordLabel,
		container.NewHBox(confirmBtn, cancelBtn),
	)

	// Create custom widget for handling mouse events
	imageContainer := &regionSelectionContainer{
		BaseWidget: widget.BaseWidget{},
		image:      fyneImage,
		selRect:    selectionRect,
		onSelectionStart: func(x, y float32) {
			selecting = true
			startX = x
			startY = y

			// Show and position the selection rectangle with initial size
			selectionRect.Move(fyne.NewPos(x, y))
			selectionRect.Resize(fyne.NewSize(5, 5))
			selectionRect.StrokeColor = color.RGBA{255, 0, 0, 255}
			selectionRect.StrokeWidth = 5
			selectionRect.FillColor = color.RGBA{255, 0, 0, 50}
			selectionRect.Show()
			selectionRect.Refresh()

			coordLabel.SetText(fmt.Sprintf("Mouse DOWN: x=%d, y=%d", int(x), int(y)))
			fmt.Printf("Selection started at: %f, %f\n", x, y)
		},
		onSelectionUpdate: func(x, y float32) {
			if selecting {
				endX = x
				endY = y

				// Update selection rectangle with red border
				rectX := min(startX, endX)
				rectY := min(startY, endY)
				rectW := abs(endX - startX)
				rectH := abs(endY - startY)

				// Make sure rectangle is visible with minimum size
				if rectW < 10 {
					rectW = 10
				}
				if rectH < 10 {
					rectH = 10
				}

				selectionRect.Move(fyne.NewPos(rectX, rectY))
				selectionRect.Resize(fyne.NewSize(rectW, rectH))
				selectionRect.StrokeColor = color.RGBA{255, 0, 0, 255}
				selectionRect.StrokeWidth = 5
				selectionRect.FillColor = color.RGBA{255, 0, 0, 50}
				selectionRect.Show()
				selectionRect.Refresh()

				// Calculate actual screen coordinates
				// Get the actual display dimensions and screen dimensions
				imageDisplaySize := fyneImage.Size()
				screenWidth := float32(bounds.Dx())
				screenHeight := float32(bounds.Dy())

				// Calculate scale factor (ImageFillContain scales to fit inside while preserving aspect ratio)
				scaleX := imageDisplaySize.Width / screenWidth
				scaleY := imageDisplaySize.Height / screenHeight
				scale := min(scaleX, scaleY) // Use smaller scale for ImageFillContain

				// Calculate the actual displayed image size
				actualImageWidth := screenWidth * scale
				actualImageHeight := screenHeight * scale

				// Calculate letterbox offsets (centering)
				offsetX := (imageDisplaySize.Width - actualImageWidth) / 2
				offsetY := (imageDisplaySize.Height - actualImageHeight) / 2

				// Adjust coordinates for letterboxing
				adjustedStartX := startX - offsetX
				adjustedStartY := startY - offsetY
				adjustedEndX := endX - offsetX
				adjustedEndY := endY - offsetY

				// Convert to screen coordinates
				actualX := int(min(adjustedStartX, adjustedEndX) / scale)
				actualY := int(min(adjustedStartY, adjustedEndY) / scale)
				actualW := int(abs(adjustedEndX-adjustedStartX) / scale)
				actualH := int(abs(adjustedEndY-adjustedStartY) / scale)

				coordLabel.SetText(fmt.Sprintf("DRAGGING: x=%d, y=%d, w=%d, h=%d",
					actualX, actualY, actualW, actualH))
				fmt.Printf("Display: %fx%f, Scale: %f, Offset: %fx%f, Coords: %d,%d,%d,%d\n",
					imageDisplaySize.Width, imageDisplaySize.Height, scale, offsetX, offsetY, actualX, actualY, actualW, actualH)
			}
		},
		onSelectionEnd: func(x, y float32) {
			if selecting {
				endX = x
				endY = y

				// Use the same calculation as onSelectionUpdate for consistency
				imageDisplaySize := fyneImage.Size()
				screenWidth := float32(bounds.Dx())
				screenHeight := float32(bounds.Dy())

				// Calculate scale factor (ImageFillContain scales to fit inside while preserving aspect ratio)
				scaleX := imageDisplaySize.Width / screenWidth
				scaleY := imageDisplaySize.Height / screenHeight
				scale := min(scaleX, scaleY) // Use smaller scale for ImageFillContain

				// Calculate the actual displayed image size
				actualImageWidth := screenWidth * scale
				actualImageHeight := screenHeight * scale

				// Calculate letterbox offsets (centering)
				offsetX := (imageDisplaySize.Width - actualImageWidth) / 2
				offsetY := (imageDisplaySize.Height - actualImageHeight) / 2

				// Adjust coordinates for letterboxing
				adjustedStartX := startX - offsetX
				adjustedStartY := startY - offsetY
				adjustedEndX := endX - offsetX
				adjustedEndY := endY - offsetY

				// Convert to screen coordinates
				actualX := int(min(adjustedStartX, adjustedEndX) / scale)
				actualY := int(min(adjustedStartY, adjustedEndY) / scale)
				actualW := int(abs(adjustedEndX-adjustedStartX) / scale)
				actualH := int(abs(adjustedEndY-adjustedStartY) / scale)

				coordLabel.SetText(fmt.Sprintf("Selected: x=%d, y=%d, w=%d, h=%d - Click Confirm to apply",
					actualX, actualY, actualW, actualH))
			}
		},
	}
	imageContainer.ExtendBaseWidget(imageContainer)

	// Make the imageContainer cover the entire scroll area for mouse events
	imageContainer.Resize(fyne.NewSize(float32(bounds.Dx()), float32(bounds.Dy())))

	contentWithImage := container.NewStack(scroll, imageContainer)
	mainContent := container.NewBorder(nil, bottom, nil, nil, contentWithImage)

	selectWindow.SetContent(mainContent)
	selectWindow.Show()
}

// regionSelectionContainer handles mouse events for region selection
type regionSelectionContainer struct {
	widget.BaseWidget
	image             *canvas.Image
	selRect           *canvas.Rectangle
	onSelectionStart  func(x, y float32)
	onSelectionUpdate func(x, y float32)
	onSelectionEnd    func(x, y float32)
	dragging          bool
}

func (r *regionSelectionContainer) MouseDown(event *desktop.MouseEvent) {
	r.dragging = true
	if r.onSelectionStart != nil {
		r.onSelectionStart(event.Position.X, event.Position.Y)
	}
}

func (r *regionSelectionContainer) MouseUp(event *desktop.MouseEvent) {
	if r.dragging {
		r.dragging = false
		if r.onSelectionEnd != nil {
			r.onSelectionEnd(event.Position.X, event.Position.Y)
		}
	}
}

func (r *regionSelectionContainer) MouseMoved(event *desktop.MouseEvent) {
	if r.dragging && r.onSelectionUpdate != nil {
		r.onSelectionUpdate(event.Position.X, event.Position.Y)
	}
}

// Add Dragged method for better drag support
func (r *regionSelectionContainer) Dragged(event *fyne.DragEvent) {
	if r.dragging && r.onSelectionUpdate != nil {
		r.onSelectionUpdate(event.Position.X, event.Position.Y)
	}
}

func (r *regionSelectionContainer) DragEnd() {
	r.dragging = false
}

func (r *regionSelectionContainer) CreateRenderer() fyne.WidgetRenderer {
	return &regionSelectionRenderer{container: r}
}

type regionSelectionRenderer struct {
	container *regionSelectionContainer
}

func (r *regionSelectionRenderer) Layout(size fyne.Size) {
	if r.container.image != nil {
		r.container.image.Resize(size)
	}
	if r.container.selRect != nil {
		// Selection rect should overlay the image
		r.container.selRect.Resize(r.container.selRect.Size())
		r.container.selRect.Move(r.container.selRect.Position())
	}
}

func (r *regionSelectionRenderer) MinSize() fyne.Size {
	return fyne.NewSize(200, 200)
}

func (r *regionSelectionRenderer) Refresh() {
	if r.container.selRect != nil {
		r.container.selRect.Refresh()
	}
}

func (r *regionSelectionRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{} // Return empty - we'll handle drawing separately
}

func (r *regionSelectionRenderer) Destroy() {}

// Helper functions
func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func abs(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func (g *GUI) openWebViewer() {
	// Start HTTP server if not already running
	go g.startWebServer()

	// Wait a moment for server to start
	time.Sleep(500 * time.Millisecond)

	// Open browser
	url := "http://localhost:8080"
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // Linux and others
		cmd = exec.Command("xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		g.addLog(fmt.Sprintf("Failed to open browser: %v", err))
		dialog.ShowError(fmt.Errorf("ブラウザを開けませんでした: %v", err), g.window)
	} else {
		g.addLog("Web viewer opened at http://localhost:8080")
	}
}

var serverStarted bool
var serverMutex sync.Mutex

func (g *GUI) startWebServer() {
	serverMutex.Lock()
	if serverStarted {
		serverMutex.Unlock()
		return
	}
	serverStarted = true
	serverMutex.Unlock()

	// Setup HTTP handlers
	http.HandleFunc("/api/regions", func(w http.ResponseWriter, r *http.Request) {
		// Load environment variables
		godotenv.Load()
		
		regions := make(map[string]string)
		for i := 1; i <= 6; i++ {
			regionName := os.Getenv(fmt.Sprintf("REGION_%d_NAME", i))
			if regionName == "" {
				regionName = fmt.Sprintf("リージョン %d", i)
			}
			regions[fmt.Sprintf("%d", i)] = regionName
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(regions)
	})
	
	// Serve web-viewer files
	http.Handle("/web-viewer/", http.StripPrefix("/web-viewer/", http.FileServer(http.Dir("web-viewer/"))))
	
	// Serve res files  
	http.Handle("/res/", http.FileServer(http.Dir("./")))
	
	// Redirect root to web-viewer
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/web-viewer/", http.StatusMovedPermanently)
		}
	})

	g.addLog("Starting web server on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		g.addLog(fmt.Sprintf("Web server error: %v", err))
		serverMutex.Lock()
		serverStarted = false
		serverMutex.Unlock()
	}
}

func runGUI() {
	gui := NewGUI()
	gui.Run()
}

func runWebServer() {
	port := os.Getenv("WEB_PORT")
	if port == "" {
		port = "8080"
	}

	// API endpoint for region names
	http.HandleFunc("/api/regions", func(w http.ResponseWriter, r *http.Request) {
		// Load environment variables
		godotenv.Load()
		
		regions := make(map[string]string)
		for i := 1; i <= 6; i++ {
			regionName := os.Getenv(fmt.Sprintf("REGION_%d_NAME", i))
			if regionName == "" {
				regionName = fmt.Sprintf("リージョン %d", i)
			}
			regions[fmt.Sprintf("%d", i)] = regionName
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(regions)
	})
	
	// Serve web-viewer files
	http.Handle("/web-viewer/", http.StripPrefix("/web-viewer/", http.FileServer(http.Dir("web-viewer/"))))
	
	// Serve res files  
	http.Handle("/res/", http.FileServer(http.Dir("./")))
	
	// Redirect root to web-viewer
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/web-viewer/", http.StatusMovedPermanently)
		}
	})

	fmt.Printf("Starting web server on port %s\n", port)
	fmt.Printf("Open http://localhost:%s to view the ranking data\n", port)

	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("Failed to start web server:", err)
	}
}

func main() {
	// Determine mode from command line arguments
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--cli":
			// CLI mode
			ctx := context.Background()
			mainLoop(ctx, []int{30})
		case "--web":
			// Web server mode
			runWebServer()
		default:
			fmt.Printf("Usage: %s [--cli|--web]\n", os.Args[0])
			fmt.Println("  --cli: Run in CLI mode")
			fmt.Println("  --web: Start web server")
			fmt.Println("  (no args): Run GUI mode")
		}
	} else {
		// GUI mode
		runGUI()
	}
}
