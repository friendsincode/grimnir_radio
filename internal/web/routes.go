/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"github.com/go-chi/chi/v5"
)

// Routes registers all web UI routes on the given router.
func (h *Handler) Routes(r chi.Router) {
	// Static files (no setup check needed)
	r.Handle("/static/*", h.StaticHandler())

	// Setup route (before RequireSetup middleware)
	r.Get("/setup", h.SetupPage)
	r.Post("/setup", h.SetupSubmit)

	// All other routes require setup to be complete
	r.Group(func(r chi.Router) {
		r.Use(h.RequireSetup)

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
			r.Get("/station/{id}", h.StationInfo)

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

			// Station selection (no station required)
			r.Get("/stations/select", h.StationSelect)
			r.Post("/stations/select", h.StationSelectSubmit)

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
					r.Get("/new", h.PlaylistNew)
					r.Post("/", h.PlaylistCreate)
					r.Get("/{id}", h.PlaylistDetail)
					r.Get("/{id}/edit", h.PlaylistEdit)
					r.Put("/{id}", h.PlaylistUpdate)
					r.Delete("/{id}", h.PlaylistDelete)
					r.Post("/{id}/items", h.PlaylistAddItem)
					r.Delete("/{id}/items/{itemID}", h.PlaylistRemoveItem)
					r.Post("/{id}/items/reorder", h.PlaylistReorderItems)
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

			// Settings (admin only)
			r.Route("/settings", func(r chi.Router) {
				r.Use(h.RequireRole("admin"))
				r.Get("/", h.SettingsPage)
				r.Put("/", h.SettingsUpdate)
				r.Get("/migrations", h.MigrationsPage)
				r.Post("/migrations/import", h.MigrationsImport)
			})
		})
	})
}
