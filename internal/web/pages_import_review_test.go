package web

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestExtractStationFilters_UsesSourceStationLabelsFromWarnings(t *testing.T) {
	staged := &models.StagedImport{
		StagedMedia: models.StagedMediaItems{
			{SourceID: "3::100"},
			{SourceID: "9::200"},
		},
		Warnings: models.ImportWarnings{
			{
				Code:     "source_station_label",
				ItemType: "station",
				ItemID:   "9",
				Message:  "Night Owl Radio",
			},
			{
				Code:     "source_station_label",
				ItemType: "station",
				ItemID:   "3",
				Message:  "RLM Main",
			},
		},
	}

	filters := extractStationFilters(staged)
	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(filters))
	}

	if filters[0].ID != "3" || filters[0].Label != "RLM Main" {
		t.Fatalf("unexpected first filter: %+v", filters[0])
	}
	if filters[1].ID != "9" || filters[1].Label != "Night Owl Radio" {
		t.Fatalf("unexpected second filter: %+v", filters[1])
	}
}

func TestExtractStationFilters_FallsBackToDescriptionAndDefaultLabel(t *testing.T) {
	staged := &models.StagedImport{
		StagedSmartBlocks: models.StagedSmartBlockItems{
			{SourceID: "5::pl1", Description: "Station: Matrix FM"},
		},
		StagedShows: models.StagedShowItems{
			{SourceID: "8::show1", Description: "Imported from station Signal Radio playlist schedule"},
		},
		StagedMedia: models.StagedMediaItems{
			{SourceID: "2::track1"},
		},
	}

	filters := extractStationFilters(staged)
	if len(filters) != 3 {
		t.Fatalf("expected 3 filters, got %d", len(filters))
	}

	if filters[0].ID != "2" || filters[0].Label != "Station 2" {
		t.Fatalf("unexpected default-labeled filter: %+v", filters[0])
	}
	if filters[1].ID != "5" || filters[1].Label != "Matrix FM" {
		t.Fatalf("unexpected smartblock-derived label: %+v", filters[1])
	}
	if filters[2].ID != "8" || filters[2].Label != "Signal Radio" {
		t.Fatalf("unexpected show-derived label: %+v", filters[2])
	}
}
