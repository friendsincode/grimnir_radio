/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import "embed"

//go:embed all:templates
var TemplateFS embed.FS

//go:embed all:static
var StaticFS embed.FS
