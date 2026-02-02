/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Routes registers all web UI routes on the given router.
func (h *Handler) Routes(r chi.Router) {
	// Static files (no setup check needed)
	r.Handle("/static/*", h.StaticHandler())

	// Favicon - simple SVG radio icon
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><circle cx="16" cy="16" r="14" fill="#6366f1"/><circle cx="16" cy="16" r="6" fill="white"/><circle cx="16" cy="16" r="2" fill="#6366f1"/></svg>`))
	})

	// Setup route (before RequireSetup middleware)
	r.Get("/setup", h.SetupPage)
	r.Post("/setup", h.SetupSubmit)

	// All other routes require setup to be complete
	r.Group(func(r chi.Router) {
		r.Use(h.RequireSetup)

		// Stream proxy (no auth needed, before other routes)
		r.Get("/stream/{station}/{mount}", h.StreamProxy)
		r.Get("/stream/{station}", h.StreamInfo)
		r.Get("/ws/stream/{station}/{mount}", h.StreamWebSocket)

		// Public routes (with optional auth context)
		r.Group(func(r chi.Router) {
			r.Use(h.AuthMiddleware)

			// Landing and public pages
			r.Get("/", h.Landing)
			r.Get("/listen", h.Listen)
			r.Get("/archive", h.Archive)
			r.Get("/archive/{id}", h.ArchiveDetail)
			r.Get("/archive/{id}/stream", h.ArchiveStream)
			r.Get("/archive/{id}/artwork", h.ArchiveArtwork)
			r.Get("/schedule", h.PublicSchedule)
			r.Get("/schedule/events", h.PublicScheduleEvents)
			r.Get("/station/{id}", h.StationInfo)

			// Embeddable widgets (Phase 8G)
			r.Get("/embed/schedule", h.EmbedSchedule)
			r.Get("/embed/now-playing", h.EmbedNowPlaying)
			r.Get("/embed/schedule.js", h.EmbedScheduleJS)

			// Auth pages
			r.Get("/login", h.LoginPage)
			r.Post("/login", h.LoginSubmit)
			r.Get("/logout", h.Logout)
		})

		// Dashboard routes (require authentication)
		r.Route("/dashboard", func(r chi.Router) {
			r.Use(h.AuthMiddleware)
			r.Use(h.RequireAuth)

			// Dashboard home
			r.Get("/", h.DashboardHome)

			// User profile (own account)
			r.Get("/profile", h.ProfilePage)
			r.Put("/profile", h.ProfileUpdate)
			r.Post("/profile/password", h.ProfileUpdatePassword)

			// API Keys management
			r.Get("/profile/api-keys", h.APIKeysSection)
			r.Post("/profile/api-keys", h.APIKeyGenerate)
			r.Delete("/profile/api-keys/{id}", h.APIKeyRevoke)

			// Station selection (no station required)
			r.Get("/stations/select", h.StationSelect)
			r.Post("/stations/select", h.StationSelectSubmit)

			// Station user management (requires station)
			r.Route("/station", func(r chi.Router) {
				r.Use(h.RequireStation)

				// Station users management
				r.Get("/users", h.StationUserList)
				r.Get("/users/invite", h.StationUserInvite)
				r.Post("/users", h.StationUserAdd)
				r.Get("/users/{id}/edit", h.StationUserEdit)
				r.Post("/users/{id}", h.StationUserUpdate)
				r.Delete("/users/{id}", h.StationUserRemove)

				// Station settings
				r.Get("/settings", h.StationSettings)
				r.Put("/settings", h.StationSettingsUpdate)
				r.Post("/settings/stop-playout", h.StationStopPlayout)

				// Station logs (all station members)
				r.Get("/logs", h.StationLogs)
			})

			// Station-scoped routes
			r.Group(func(r chi.Router) {
				r.Use(h.RequireStation)

				// Stations management (manager+)
				r.Route("/stations", func(r chi.Router) {
					r.Use(h.RequireRole("manager"))
					r.Get("/", h.StationsList)
					r.Get("/new", h.StationNew)
					r.Post("/", h.StationCreate)
					r.Get("/{id}", h.StationEdit)
					r.Put("/{id}", h.StationUpdate)
					r.Delete("/{id}", h.StationDelete)

					// Mounts (admin only)
					r.Route("/{stationID}/mounts", func(r chi.Router) {
						r.Use(h.RequireRole("admin"))
						r.Get("/", h.MountsList)
						r.Get("/new", h.MountNew)
						r.Post("/", h.MountCreate)
						r.Get("/{id}", h.MountEdit)
						r.Put("/{id}", h.MountUpdate)
						r.Delete("/{id}", h.MountDelete)
					})
				})

				// Media library
				r.Route("/media", func(r chi.Router) {
					r.Get("/", h.MediaList)
					r.Post("/bulk", h.MediaBulk)
					r.Get("/upload", h.MediaUploadPage)
					r.Post("/upload", h.MediaUpload)
					r.Get("/{id}", h.MediaDetail)
					r.Get("/{id}/edit", h.MediaEdit)
					r.Put("/{id}", h.MediaUpdate)
					r.Delete("/{id}", h.MediaDelete)
					r.Get("/{id}/waveform", h.MediaWaveform)
					r.Get("/{id}/artwork", h.MediaArtwork)
					r.Get("/{id}/stream", h.MediaStream)

					// HTMX partials
					r.Get("/table", h.MediaTablePartial)
					r.Get("/grid", h.MediaGridPartial)
				})

				// Playlists
				r.Route("/playlists", func(r chi.Router) {
					r.Get("/", h.PlaylistList)
					r.Post("/bulk", h.PlaylistBulk)
					r.Get("/new", h.PlaylistNew)
					r.Post("/", h.PlaylistCreate)
					r.Get("/{id}", h.PlaylistDetail)
					r.Get("/{id}/edit", h.PlaylistEdit)
					r.Put("/{id}", h.PlaylistUpdate)
					r.Delete("/{id}", h.PlaylistDelete)
					r.Post("/{id}/items", h.PlaylistAddItem)
					r.Delete("/{id}/items/{itemID}", h.PlaylistRemoveItem)
					r.Post("/{id}/items/reorder", h.PlaylistReorderItems)
					r.Get("/{id}/cover", h.PlaylistCover)
					r.Post("/{id}/cover", h.PlaylistUploadCover)
					r.Delete("/{id}/cover", h.PlaylistDeleteCover)
					r.Get("/{id}/media-search", h.PlaylistMediaSearch)
				})

				// Smart Blocks
				r.Route("/smart-blocks", func(r chi.Router) {
					r.Use(h.RequireRole("manager"))
					r.Get("/", h.SmartBlockList)
					r.Get("/new", h.SmartBlockNew)
					r.Post("/", h.SmartBlockCreate)
					r.Get("/{id}", h.SmartBlockDetail)
					r.Get("/{id}/edit", h.SmartBlockEdit)
					r.Put("/{id}", h.SmartBlockUpdate)
					r.Delete("/{id}", h.SmartBlockDelete)
					r.Post("/{id}/preview", h.SmartBlockPreview)
					r.Post("/{id}/duplicate", h.SmartBlockDuplicate)
				})

				// Clock Templates
				r.Route("/clocks", func(r chi.Router) {
					r.Use(h.RequireRole("manager"))
					r.Get("/", h.ClockList)
					r.Get("/new", h.ClockNew)
					r.Post("/", h.ClockCreate)
					r.Get("/{id}", h.ClockDetail)
					r.Get("/{id}/edit", h.ClockEdit)
					r.Put("/{id}", h.ClockUpdate)
					r.Delete("/{id}", h.ClockDelete)
					r.Post("/{id}/simulate", h.ClockSimulate)
				})

				// Schedule
				r.Route("/schedule", func(r chi.Router) {
					r.Get("/", h.ScheduleCalendar)
					r.Get("/events", h.ScheduleEvents) // JSON for calendar
					r.Post("/entries", h.ScheduleCreateEntry)
					r.Put("/entries/{id}", h.ScheduleUpdateEntry)
					r.Delete("/entries/{id}", h.ScheduleDeleteEntry)
					r.Post("/refresh", h.ScheduleRefresh)

					// JSON endpoints for schedule dropdowns
					r.Get("/playlists.json", h.SchedulePlaylistsJSON)
					r.Get("/smart-blocks.json", h.ScheduleSmartBlocksJSON)
					r.Get("/clocks.json", h.ScheduleClocksJSON)
					r.Get("/webstreams.json", h.ScheduleWebstreamsJSON)
					r.Get("/media.json", h.ScheduleMediaSearchJSON)

					// Show instance events for calendar
					r.Get("/show-events", h.ShowInstanceEvents)
				})

				// Shows (Phase 8 - Advanced Scheduling)
				r.Route("/shows", func(r chi.Router) {
					r.Use(h.RequireRole("manager"))
					r.Get("/", h.ShowsJSON)
					r.Post("/", h.ShowCreate)
					r.Put("/{id}", h.ShowUpdate)
					r.Delete("/{id}", h.ShowDelete)
					r.Post("/{id}/materialize", h.ShowMaterialize)

					// Show instances
					r.Put("/instances/{id}", h.ShowInstanceUpdate)
					r.Delete("/instances/{id}", h.ShowInstanceCancel)
				})

				// DJ Self-Service (Phase 8E)
				r.Route("/dj", func(r chi.Router) {
					r.Get("/availability", h.DJAvailability)
					r.Get("/availability.json", h.DJAvailabilityJSON)
					r.Get("/requests", h.DJRequests)
				})

				// Live DJ
				r.Route("/live", func(r chi.Router) {
					r.Get("/", h.LiveDashboard)
					r.Get("/sessions", h.LiveSessions)
					r.Post("/tokens", h.LiveGenerateToken)
					r.Post("/connect", h.LiveConnect)
					r.Delete("/sessions/{id}", h.LiveDisconnect)
					r.Post("/handover", h.LiveHandover)
					r.Delete("/handover", h.LiveReleaseHandover)
				})

				// Webstreams
				r.Route("/webstreams", func(r chi.Router) {
					r.Use(h.RequireRole("manager"))
					r.Get("/", h.WebstreamList)
					r.Get("/new", h.WebstreamNew)
					r.Post("/", h.WebstreamCreate)
					r.Get("/{id}", h.WebstreamDetail)
					r.Get("/{id}/edit", h.WebstreamEdit)
					r.Put("/{id}", h.WebstreamUpdate)
					r.Delete("/{id}", h.WebstreamDelete)
					r.Post("/{id}/failover", h.WebstreamFailover)
					r.Post("/{id}/reset", h.WebstreamReset)
				})

				// Playout controls
				r.Route("/playout", func(r chi.Router) {
					r.Post("/skip", h.PlayoutSkip)
					r.Post("/stop", h.PlayoutStop)
					r.Post("/reload", h.PlayoutReload)
				})

				// Analytics
				r.Route("/analytics", func(r chi.Router) {
					r.Get("/", h.AnalyticsDashboard)
					r.Get("/now-playing", h.AnalyticsNowPlaying)
					r.Get("/history", h.AnalyticsHistory)
					r.Get("/spins", h.AnalyticsSpins)
					r.Get("/listeners", h.AnalyticsListeners)
				})
			})

			// User management (manager+ for DJs, admin for all)
			r.Route("/users", func(r chi.Router) {
				r.Use(h.RequireRole("manager"))
				r.Get("/", h.UserList)
				r.Get("/new", h.UserNew)
				r.Post("/", h.UserCreate)
				r.Get("/{id}", h.UserDetail)
				r.Get("/{id}/edit", h.UserEdit)
				r.Put("/{id}", h.UserUpdate)
				r.Delete("/{id}", h.UserDelete)
			})

			// Settings (platform admin only)
			r.Route("/settings", func(r chi.Router) {
				r.Use(h.RequireRole("admin"))
				r.Get("/", h.SettingsPage)
				r.Put("/", h.SettingsUpdate)
				r.Get("/migrations", h.MigrationsPage)
				r.Get("/migrations/status", h.MigrationStatusPage)
				r.Post("/migrations/import", h.MigrationsImport)
				r.Post("/migrations/azuracast-api", h.AzuraCastAPIImport)
				r.Post("/migrations/azuracast-api/test", h.AzuraCastAPITest)
				r.Post("/migrations/libretime-api", h.LibreTimeAPIImport)
				r.Post("/migrations/libretime-api/test", h.LibreTimeAPITest)
				r.Post("/migrations/jobs/{id}/restart", h.MigrationJobRestart)
				r.Delete("/migrations/jobs/{id}", h.MigrationJobDelete)
				r.Post("/migrations/reset", h.MigrationResetData)
			})

			// Platform Admin routes (platform_admin only)
			r.Route("/admin", func(r chi.Router) {
				r.Use(h.RequirePlatformAdmin)

				// All stations management
				r.Get("/stations", h.AdminStationsList)
				r.Post("/stations/bulk", h.AdminStationsBulk)
				r.Post("/stations/{id}/toggle-active", h.AdminStationToggleActive)
				r.Post("/stations/{id}/toggle-public", h.AdminStationTogglePublic)
				r.Post("/stations/{id}/toggle-approved", h.AdminStationToggleApproved)

				// All users management
				r.Get("/users", h.AdminUsersList)
				r.Post("/users/bulk", h.AdminUsersBulk)
				r.Get("/users/{id}/edit", h.AdminUserEdit)
				r.Post("/users/{id}", h.AdminUserUpdate)
				r.Delete("/users/{id}", h.AdminUserDelete)

				// Platform media library
				r.Get("/media", h.AdminMediaList)
				r.Post("/media/bulk", h.AdminMediaBulk)
				r.Post("/media/{id}/toggle-public", h.AdminMediaTogglePublic)
				r.Post("/media/{id}/move", h.AdminMediaMove)
				r.Delete("/media/{id}", h.AdminMediaDelete)

				// System logs
				r.Get("/logs", h.AdminLogs)
			})
		})
	})
}
