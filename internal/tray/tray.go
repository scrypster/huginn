// Package tray implements the huginn menu bar / system tray application.
// It owns the Huginn HTTP+WebSocket server process: starting it on tray
// launch and stopping it on tray exit.
//
// Build requirement: CGo must be enabled (CGO_ENABLED=1) on macOS and Linux.
// macOS: links against Cocoa/AppKit (NSApplication run loop).
// Linux: links against GTK3 + libayatana-appindicator3.
// Windows: uses Win32 Shell_NotifyIcon; CGo not required.
package tray

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/scrypster/huginn/internal/logger"
)

// Config holds the parameters the tray needs to manage the server and satellite.
type Config struct {
	// Port is the configured port. Used to build the web UI URL.
	Port int

	// HuginnBinPath is the absolute path to the huginn binary.
	// Used for terminal launch. Obtain via os.Executable() at call site.
	HuginnBinPath string

	// OnStart is called by the tray to start the server.
	// Returns the listening address (e.g. "127.0.0.1:8421") or an error.
	OnStart func() (addr string, err error)

	// OnStop is called by the tray to stop the server.
	// Only called if the tray started the server (serverOwned == true).
	OnStop func()

	// OnSatelliteConnect is called to bring the satellite online.
	// Returns an error if the connection fails (e.g. HuginnCloud unreachable).
	OnSatelliteConnect func() error

	// OnSatelliteDisconnect is called to take the satellite offline.
	OnSatelliteDisconnect func()

	// OnSatelliteStatus returns whether the satellite is currently connected.
	OnSatelliteStatus func() bool

	// AttachAddr, if non-empty, skips OnStart and connects to an already-running
	// server at this address (e.g. "127.0.0.1:8421"). Used when huginn serve
	// spawns the tray as a subprocess rather than the tray starting the server.
	AttachAddr string
}

var (
	globalCfg Config

	stateMu         sync.Mutex
	serverAddr      string
	serverOwned     bool
	satelliteOnline bool
	hidingOnly      bool // true when hiding icon without stopping server
)

// Run starts the system tray. Blocks until the user selects Quit.
// Call from cmdTray() in main.go.
func Run(cfg Config) {
	globalCfg = cfg
	systray.Run(onReady, onExit)
}

// updateTrayIcon sets the tray icon based on current server and satellite state.
// Uses SetTemplateIcon (pure black, adapts to dark/light) on Darwin when no
// color is needed; SetIcon (colored) otherwise.
// Must NOT be called with stateMu held (systray calls must not happen under the lock).
func updateTrayIcon() {
	stateMu.Lock()
	satOnline := satelliteOnline
	owned := serverOwned
	stateMu.Unlock()

	switch {
	case satOnline:
		setTrayIcon(iconCloud)
	case owned:
		setTrayIcon(iconRunning)
	default:
		if runtime.GOOS == "darwin" {
			systray.SetTemplateIcon(iconDefault, iconDefault)
		} else {
			setTrayIcon(iconDefault)
		}
	}
}

func onReady() {
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(iconDefault, iconDefault)
	} else {
		setTrayIcon(iconDefault)
	}
	systray.SetTitle("")
	systray.SetTooltip("Huginn — starting…")

	// ── Quick access ───────────────────────────────────────────
	mOpenUI := systray.AddMenuItem("Open Dashboard", "Open the Huginn dashboard in your browser")

	// Build terminal menu — submenu if multiple terminals detected, plain item if one.
	terminals := DetectTerminals()
	var mNewTerm *systray.MenuItem
	if len(terminals) <= 1 {
		name := "Terminal"
		if len(terminals) == 1 {
			name = terminals[0].Name
		}
		mNewTerm = systray.AddMenuItem("Open "+name, "Open a terminal window")
	} else {
		mNewTerm = systray.AddMenuItem("Open Terminal", "Open a terminal window")
		for _, t := range terminals {
			t := t
			sub := mNewTerm.AddSubMenuItem(t.Name, "Open "+t.Name)
			go func() {
				for range sub.ClickedCh {
					_ = OpenTerminal(t.Name)
				}
			}()
		}
	}

	systray.AddSeparator()

	mToggle := systray.AddMenuItem("Stop Huginn", "Stop Huginn on this machine (routines will not run)")
	mCloudToggle := systray.AddMenuItem("Connect to HuginnCloud", "Sync this machine with your HuginnCloud account")

	systray.AddSeparator()

	mHide := systray.AddMenuItem("Hide Menu Bar Icon", "Remove from menu bar — Huginn keeps running")
	mQuit := systray.AddMenuItem("Quit Huginn", "Stop Huginn and quit")

	// Start on tray launch — or attach to an already-running server.
	if globalCfg.AttachAddr != "" {
		stateMu.Lock()
		serverAddr = globalCfg.AttachAddr
		serverOwned = true
		stateMu.Unlock()
		systray.SetTooltip("Huginn — running")
		mToggle.SetTitle("Stop Huginn")
		updateTrayIcon()
	} else {
		go func() {
			addr, err := globalCfg.OnStart()
			if err != nil {
				systray.SetTooltip("Huginn — failed to start: " + err.Error())
				mToggle.SetTitle("Start Huginn")
				return
			}
			stateMu.Lock()
			serverAddr = addr
			serverOwned = true
			stateMu.Unlock()
			systray.SetTooltip("Huginn — running")
			mToggle.SetTitle("Stop Huginn")
			updateTrayIcon()
		}()
	}

	// Health poll — update server status every 5 seconds.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			stateMu.Lock()
			addr := serverAddr
			stateMu.Unlock()
			if addr == "" {
				continue
			}
			healthURL := fmt.Sprintf("http://%s/api/v1/health", addr)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
				systray.SetTooltip("Huginn — not responding")
				mToggle.SetTitle("Start Huginn")
				stateMu.Lock()
				serverOwned = false
				stateMu.Unlock()
				updateTrayIcon()
			} else {
				systray.SetTooltip("Huginn — running")
				mToggle.SetTitle("Stop Huginn")
				// Read satellite_connected from health response.
				var health struct {
					SatelliteConnected bool `json:"satellite_connected"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
					slog.Warn("tray: health: decode failed", "err", err)
				}
				nowSat := health.SatelliteConnected
				stateMu.Lock()
				prevSat := satelliteOnline
				if nowSat != prevSat {
					satelliteOnline = nowSat
				}
				stateMu.Unlock()
				if nowSat != prevSat {
					if nowSat {
						mCloudToggle.SetTitle("● HuginnCloud — Disconnect")
					} else {
						mCloudToggle.SetTitle("Connect to HuginnCloud")
					}
					updateTrayIcon()
				}
			}
			if resp != nil {
				resp.Body.Close()
			}

			// Reflect live satellite status if a checker is wired (non-attach mode).
			if globalCfg.OnSatelliteStatus != nil {
				if globalCfg.OnSatelliteStatus() {
					stateMu.Lock()
					satelliteOnline = true
					stateMu.Unlock()
					mCloudToggle.SetTitle("● HuginnCloud — Disconnect")
					updateTrayIcon()
				} else {
					stateMu.Lock()
					wasSat := satelliteOnline
					if wasSat {
						// Was connected, now dropped.
						satelliteOnline = false
					}
					stateMu.Unlock()
					if wasSat {
						mCloudToggle.SetTitle("Connect to HuginnCloud")
						updateTrayIcon()
					}
				}
			}
		}
	}()

	// Event loop.
	go func() {
		for {
			select {
			case <-mOpenUI.ClickedCh:
				stateMu.Lock()
				addr := serverAddr
				stateMu.Unlock()
				if addr != "" {
					_ = openURL(fmt.Sprintf("http://%s", addr))
				}
			case <-mNewTerm.ClickedCh:
				// Only fires when there's a single terminal (no submenu).
				name := "Terminal"
				if len(terminals) == 1 {
					name = terminals[0].Name
				}
				if err := OpenTerminal(name); err != nil {
					logger.Warn("tray: open terminal failed", "terminal", name, "err", err)
				}
			case <-mToggle.ClickedCh:
				stateMu.Lock()
				owned := serverOwned
				stateMu.Unlock()
				if owned {
					if globalCfg.OnStop != nil {
						globalCfg.OnStop()
					}
					stateMu.Lock()
					serverOwned = false
					serverAddr = ""
					stateMu.Unlock()
					systray.SetTooltip("Huginn — stopped")
					mToggle.SetTitle("Start Huginn")
					updateTrayIcon()
				} else {
					systray.SetTooltip("Huginn — starting…")
					addr, err := globalCfg.OnStart()
					if err != nil {
						systray.SetTooltip("Huginn — failed to start: " + err.Error())
						mToggle.SetTitle("Start Huginn")
						continue
					}
					stateMu.Lock()
					serverAddr = addr
					serverOwned = true
					stateMu.Unlock()
					systray.SetTooltip("Huginn — running")
					mToggle.SetTitle("Stop Huginn")
					updateTrayIcon()
				}
			case <-mCloudToggle.ClickedCh:
				stateMu.Lock()
				satOnline := satelliteOnline
				stateMu.Unlock()
				if satOnline {
					// Take offline.
					if globalCfg.OnSatelliteDisconnect != nil {
						globalCfg.OnSatelliteDisconnect()
					}
					stateMu.Lock()
					satelliteOnline = false
					stateMu.Unlock()
					mCloudToggle.SetTitle("Connect to HuginnCloud")
					updateTrayIcon()
				} else {
					// Attempt to connect.
					mCloudToggle.SetTitle("◌ Connecting…")
					mCloudToggle.Disable()
					go func() {
						var connectErr error
						if globalCfg.OnSatelliteConnect != nil {
							connectErr = globalCfg.OnSatelliteConnect()
						} else {
							// No satellite wired — simulate unreachable.
							time.Sleep(1 * time.Second)
							connectErr = fmt.Errorf("unable to reach HuginnCloud")
						}
						mCloudToggle.Enable()
						if connectErr != nil {
							mCloudToggle.SetTitle("⊘ HuginnCloud unreachable — Try Again")
							mCloudToggle.SetTooltip(connectErr.Error())
						} else {
							stateMu.Lock()
							satelliteOnline = true
							stateMu.Unlock()
							mCloudToggle.SetTitle("● HuginnCloud — Disconnect")
							updateTrayIcon()
						}
					}()
				}
			case <-mHide.ClickedCh:
				// Remove icon from menu bar; server and satellite keep running.
				// Relaunch `huginn tray` to restore the icon.
				stateMu.Lock()
				hidingOnly = true
				stateMu.Unlock()
				systray.Quit()
				return
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	stateMu.Lock()
	hiding := hidingOnly
	satOnline := satelliteOnline
	owned := serverOwned
	stateMu.Unlock()

	if hiding {
		// Icon hidden but server/satellite keep running — nothing to stop.
		return
	}
	if satOnline && globalCfg.OnSatelliteDisconnect != nil {
		globalCfg.OnSatelliteDisconnect()
	}
	if owned && globalCfg.OnStop != nil {
		globalCfg.OnStop()
	}
}
