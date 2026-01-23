/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package dsp

import (
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func TestBuilder_Build(t *testing.T) {
	logger := zerolog.Nop()
	builder := NewBuilder(logger)

	tests := []struct {
		name    string
		graph   *pb.DSPGraph
		wantErr bool
	}{
		{
			name: "simple linear pipeline",
			graph: &pb.DSPGraph{
				Nodes: []*pb.DSPNode{
					{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
					{Id: "loudness", Type: pb.NodeType_NODE_TYPE_LOUDNESS_NORMALIZE},
					{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
				},
				Connections: []*pb.DSPConnection{
					{FromNode: "input", ToNode: "loudness"},
					{FromNode: "loudness", ToNode: "output"},
				},
			},
			wantErr: false,
		},
		{
			name: "complex processing chain",
			graph: &pb.DSPGraph{
				Nodes: []*pb.DSPNode{
					{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
					{Id: "gate", Type: pb.NodeType_NODE_TYPE_GATE, Params: map[string]string{"threshold": "-60"}},
					{Id: "eq", Type: pb.NodeType_NODE_TYPE_EQUALIZER, Params: map[string]string{"bands": "10"}},
					{Id: "comp", Type: pb.NodeType_NODE_TYPE_COMPRESSOR, Params: map[string]string{"threshold": "-20", "ratio": "4"}},
					{Id: "limiter", Type: pb.NodeType_NODE_TYPE_LIMITER, Params: map[string]string{"threshold": "-1"}},
					{Id: "meter", Type: pb.NodeType_NODE_TYPE_LEVEL_METER},
					{Id: "output", Type: pb.NodeType_NODE_TYPE_OUTPUT},
				},
				Connections: []*pb.DSPConnection{
					{FromNode: "input", ToNode: "gate"},
					{FromNode: "gate", ToNode: "eq"},
					{FromNode: "eq", ToNode: "comp"},
					{FromNode: "comp", ToNode: "limiter"},
					{FromNode: "limiter", ToNode: "meter"},
					{FromNode: "meter", ToNode: "output"},
				},
			},
			wantErr: false,
		},
		{
			name:    "nil graph",
			graph:   nil,
			wantErr: true,
		},
		{
			name: "empty graph",
			graph: &pb.DSPGraph{
				Nodes: []*pb.DSPNode{},
			},
			wantErr: true,
		},
		{
			name: "duplicate node IDs",
			graph: &pb.DSPGraph{
				Nodes: []*pb.DSPNode{
					{Id: "node1", Type: pb.NodeType_NODE_TYPE_INPUT},
					{Id: "node1", Type: pb.NodeType_NODE_TYPE_OUTPUT},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid connection reference",
			graph: &pb.DSPGraph{
				Nodes: []*pb.DSPNode{
					{Id: "input", Type: pb.NodeType_NODE_TYPE_INPUT},
				},
				Connections: []*pb.DSPConnection{
					{FromNode: "input", ToNode: "nonexistent"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := builder.Build(tt.graph)
			if (err != nil) != tt.wantErr {
				t.Errorf("Builder.Build() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if graph == nil {
					t.Error("Builder.Build() returned nil graph")
					return
				}
				if graph.Pipeline == "" {
					t.Error("Builder.Build() returned empty pipeline")
				}
			}
		})
	}
}

func TestBuilder_BuildNode(t *testing.T) {
	logger := zerolog.Nop()
	builder := NewBuilder(logger)

	tests := []struct {
		name     string
		node     *pb.DSPNode
		wantErr  bool
		contains string // substring that should be in the output
	}{
		{
			name:     "loudness normalize node",
			node:     &pb.DSPNode{Id: "loud", Type: pb.NodeType_NODE_TYPE_LOUDNESS_NORMALIZE, Params: map[string]string{"target_lufs": "-16"}},
			wantErr:  false,
			contains: "rgvolume",
		},
		{
			name:     "AGC node",
			node:     &pb.DSPNode{Id: "agc", Type: pb.NodeType_NODE_TYPE_AGC, Params: map[string]string{"target_level": "-20", "max_gain": "12"}},
			wantErr:  false,
			contains: "audioamplify",
		},
		{
			name:     "compressor node",
			node:     &pb.DSPNode{Id: "comp", Type: pb.NodeType_NODE_TYPE_COMPRESSOR, Params: map[string]string{"threshold": "-20", "ratio": "4"}},
			wantErr:  false,
			contains: "ladspa-sc4",
		},
		{
			name:     "limiter node",
			node:     &pb.DSPNode{Id: "lim", Type: pb.NodeType_NODE_TYPE_LIMITER, Params: map[string]string{"threshold": "-1"}},
			wantErr:  false,
			contains: "audiodynamic",
		},
		{
			name:     "equalizer node",
			node:     &pb.DSPNode{Id: "eq", Type: pb.NodeType_NODE_TYPE_EQUALIZER, Params: map[string]string{"bands": "10"}},
			wantErr:  false,
			contains: "equalizer-nbands",
		},
		{
			name:     "gate node",
			node:     &pb.DSPNode{Id: "gate", Type: pb.NodeType_NODE_TYPE_GATE, Params: map[string]string{"threshold": "-60"}},
			wantErr:  false,
			contains: "audiodynamic",
		},
		{
			name:     "silence detector node",
			node:     &pb.DSPNode{Id: "silence", Type: pb.NodeType_NODE_TYPE_SILENCE_DETECTOR, Params: map[string]string{"threshold": "-50"}},
			wantErr:  false,
			contains: "silencedetect",
		},
		{
			name:     "level meter node",
			node:     &pb.DSPNode{Id: "meter", Type: pb.NodeType_NODE_TYPE_LEVEL_METER, Params: map[string]string{"interval_ms": "100"}},
			wantErr:  false,
			contains: "level",
		},
		{
			name:     "mix node",
			node:     &pb.DSPNode{Id: "mix", Type: pb.NodeType_NODE_TYPE_MIX},
			wantErr:  false,
			contains: "audiomixer",
		},
		{
			name:    "unsupported node type",
			node:    &pb.DSPNode{Id: "unknown", Type: pb.NodeType_NODE_TYPE_UNSPECIFIED},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			element, err := builder.buildNode(tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("Builder.buildNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if element == "" {
					t.Error("Builder.buildNode() returned empty element")
					return
				}
				if tt.contains != "" && !containsSubstring(element, tt.contains) {
					t.Errorf("Builder.buildNode() = %v, want to contain %v", element, tt.contains)
				}
			}
		})
	}
}

func TestGetParamHelpers(t *testing.T) {
	params := map[string]string{
		"string_val": "test",
		"float_val":  "3.14",
		"int_val":    "42",
		"invalid":    "not_a_number",
	}

	t.Run("getParam", func(t *testing.T) {
		if got := getParam(params, "string_val", "default"); got != "test" {
			t.Errorf("getParam() = %v, want %v", got, "test")
		}
		if got := getParam(params, "missing", "default"); got != "default" {
			t.Errorf("getParam() = %v, want %v", got, "default")
		}
	})

	t.Run("getParamFloat", func(t *testing.T) {
		if got := getParamFloat(params, "float_val", 0.0); got != 3.14 {
			t.Errorf("getParamFloat() = %v, want %v", got, 3.14)
		}
		if got := getParamFloat(params, "missing", 1.5); got != 1.5 {
			t.Errorf("getParamFloat() = %v, want %v", got, 1.5)
		}
		if got := getParamFloat(params, "invalid", 2.0); got != 2.0 {
			t.Errorf("getParamFloat() = %v, want %v", got, 2.0)
		}
	})

	t.Run("getParamInt", func(t *testing.T) {
		if got := getParamInt(params, "int_val", 0); got != 42 {
			t.Errorf("getParamInt() = %v, want %v", got, 42)
		}
		if got := getParamInt(params, "missing", 10); got != 10 {
			t.Errorf("getParamInt() = %v, want %v", got, 10)
		}
		if got := getParamInt(params, "invalid", 20); got != 20 {
			t.Errorf("getParamInt() = %v, want %v", got, 20)
		}
	})
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
