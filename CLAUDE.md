# UNI'S ON AIR Speed Tracker

A Go application that captures screenshots of game rankings, extracts data using Gemini AI, and tracks player point changes over time.

## Project Overview

This application monitors gaming leaderboards by:
- Taking automated screenshots of specified screen regions
- Using Google's Gemini AI to extract ranking data from images
- Tracking point changes over different time periods (1h, 6h, 12h, 24h)
- Sending results to Discord webhooks
- Saving data in JSON and CSV formats

## Key Features

- **GUI Interface**: Built with Fyne framework for easy configuration
- **Automated Screenshot Capture**: Configurable screen regions with visual selection tool
- **AI-Powered OCR**: Uses Gemini 1.5 Flash for text extraction from images
- **Time-Based Analysis**: Tracks point differences across multiple time periods
- **Multi-format Export**: Saves data as JSON and CSV files
- **Discord Integration**: Sends formatted results to Discord channels

## Tech Stack

- **Language**: Go 1.21
- **GUI Framework**: Fyne v2.4.3
- **AI Service**: Google Generative AI (Gemini)
- **Screenshot Library**: kbinani/screenshot
- **Environment**: godotenv for configuration

## Project Structure

```
├── main.go              # Main application code
├── go.mod              # Go module dependencies
├── go.sum              # Dependency checksums
├── .env                # Environment configuration (created at runtime)
├── config.json         # Name replacement mappings
├── res/                # Output directory for data
│   ├── {index}/
│   │   ├── screenshot/ # Screenshot images
│   │   ├── json/       # JSON data files
│   │   └── csv/        # CSV exports
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

1. **Screenshot Capture**: Captures specified screen regions
2. **AI Processing**: Sends images to Gemini for text extraction
3. **Data Processing**: Parses ranking data and calculates point differences
4. **Storage**: Saves to JSON/CSV and sends to Discord
5. **Scheduling**: Repeats at configured intervals

## Error Handling

- Graceful handling of missing environment variables
- Fallback behavior for OCR failures
- Comprehensive logging through GUI interface
- Validation of screen regions and configuration parameters