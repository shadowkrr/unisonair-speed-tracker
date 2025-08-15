# UNI'S ON AIR Speed Tracker

A Go application that captures screenshots of game rankings, extracts data using Gemini AI, and tracks player point changes over time.

## Project Overview

This application monitors gaming leaderboards by:
- **Automated UI Navigation**: Uses image matching to navigate to ranking screens
- Taking automated screenshots of specified screen regions
- Using Google's Gemini AI to extract ranking data from images
- Tracking point changes over different time periods (1h, 6h, 12h, 24h)
- Sending results to Discord webhooks
- Saving data in JSON and CSV formats

## Key Features

- **GUI Interface**: Built with Fyne framework for easy configuration
- **Image-Based UI Automation**: Automatically navigates to ranking screens using image matching
- **Automated Screenshot Capture**: Configurable screen regions with visual selection tool
- **AI-Powered OCR**: Uses Gemini 1.5 Flash for text extraction from images
- **Time-Based Analysis**: Tracks point differences across multiple time periods
- **Multi-format Export**: Saves data as JSON and CSV files
- **Discord Integration**: Sends formatted results to Discord channels
- **Retry Logic**: Automatically retries until ranking screens are accessible

## Tech Stack

- **Language**: Go 1.21
- **GUI Framework**: Fyne v2.4.3
- **AI Service**: Google Generative AI (Gemini)
- **Image Matching**: Python with pyautogui for UI automation
- **Screenshot Library**: kbinani/screenshot
- **Mouse Simulation**: Windows PowerShell for click automation
- **Environment**: godotenv for configuration

## Project Structure

```
├── main.go              # Main application code with UI automation
├── image_matcher.py     # Python script for image matching and detection
├── go.mod              # Go module dependencies
├── go.sum              # Dependency checksums
├── .env                # Environment configuration (created at runtime)
├── config.json         # Name replacement mappings
├── res/
│   ├── image/          # UI element images for automation
│   │   ├── all_ranking.png     # Overall ranking button image
│   │   ├── reward_ranking.png  # Ranking reward button image
│   │   ├── ranking.png         # Ranking button image
│   │   └── top_ranking.png     # Top ranking button image
│   └── {index}/        # Output directory for data
│       ├── screenshot/ # Screenshot images
│       ├── json/       # JSON data files
│       └── csv/        # CSV exports
└── CLAUDE.md           # This documentation file
```

## Configuration

The application uses environment variables for configuration:

### Required
- `GEMINI_API_KEY`: Google Gemini API key for OCR processing

### Optional
- `DISCORD_WEBHOOK_0-4`: Discord webhook URLs for different regions
- `REGION_0-4`: Screen capture regions in format "x,y,width,height"
- `DESIRED_MINUTES`: Comma-separated execution times (e.g., "1,15,30")

## Usage

### GUI Mode (Default)
```bash
go run main.go
```

### CLI Mode
```bash
go run main.go --cli
```

## Build Commands

```bash
# Build for current platform
go build -o unisonair-speed-tracker.exe

# Run with dependencies
go mod tidy
go run main.go

# Test build
go build -v
```

## Development Notes

- The application automatically detects screen dimensions for Region 0 (full screen)
- Screenshots are saved with timestamp format: `YYYYMMDDHHMM.png`
- JSON data structure preserves ranking history for time-based analysis
- CSV export includes extended time periods (1h, 3h, 6h, 9h, 12h, 15h, 18h, 21h, 24h)
- Name replacement feature allows mapping player names via config.json

## Data Flow

1. **UI Navigation**: Automatically navigates to ranking screens using image matching
   - Searches for and clicks "総合ランキングボタン" (Overall Ranking button)
   - Searches for and clicks "ランキング報酬ボタン" (Ranking Reward button)
   - Searches for and clicks "ランキングボタン" (Ranking button)
   - Retries until "上位ランキングボタン" (Top Ranking button) is found and clicked
2. **Screenshot Capture**: Captures specified screen regions
3. **AI Processing**: Sends images to Gemini for text extraction
4. **Data Processing**: Parses ranking data and calculates point differences
5. **Storage**: Saves to JSON/CSV and sends to Discord
6. **Scheduling**: Repeats at configured intervals

## Error Handling

- Graceful handling of missing environment variables
- Fallback behavior for OCR failures
- **Image Matching Retry Logic**: Continuously retries UI navigation until successful
- **Smart Clicking**: Only clicks when images are actually found on screen
- **Fallback Coordinates**: Uses predefined coordinates when images are detected
- Comprehensive logging through GUI interface
- Validation of screen regions and configuration parameters

## UI Automation Features

### Image Matching System
- Uses Python's `pyautogui` for reliable image detection
- Confidence-based matching for accurate button recognition
- JSON output format for seamless Go integration

### Navigation Sequence
The application follows a specific sequence to reach the ranking data:
1. **Overall Ranking** → **Ranking Rewards** → **Rankings** → **Top Rankings**
2. Each step waits for the UI to load before proceeding
3. Automatically retries the entire sequence if the final "Top Rankings" button is not found
4. Only proceeds to data capture once navigation is successful

### Click Behavior
- **Image Detection**: First searches for the target UI element
- **Fallback Coordinates**: If image is found, clicks predefined coordinates for reliability
- **No Image, No Click**: Never clicks if the target image is not detected
- **Retry Logic**: Repeats navigation sequence until successful

### Dependencies
- **Python**: Required for image matching (`pyautogui`, `opencv-python`)
- **PowerShell**: Used for mouse click simulation on Windows