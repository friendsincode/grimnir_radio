package dsp

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// Graph represents a compiled DSP processing graph
type Graph struct {
	ID       string
	Pipeline string // GStreamer pipeline description
	Nodes    []*pb.DSPNode
	logger   zerolog.Logger
}

// Builder constructs GStreamer pipelines from DSP graph protobuf definitions
type Builder struct {
	logger zerolog.Logger
}

// NewBuilder creates a new DSP graph builder
func NewBuilder(logger zerolog.Logger) *Builder {
	return &Builder{
		logger: logger,
	}
}

// Build converts a DSP graph protobuf into a GStreamer pipeline string
func (b *Builder) Build(graphProto *pb.DSPGraph) (*Graph, error) {
	if graphProto == nil {
		return nil, fmt.Errorf("graph proto is nil")
	}

	b.logger.Debug().Int("node_count", len(graphProto.Nodes)).Msg("building DSP graph")

	// Validate graph
	if err := b.validateGraph(graphProto); err != nil {
		return nil, fmt.Errorf("invalid graph: %w", err)
	}

	// Build pipeline elements
	elements := make(map[string]string)
	for _, node := range graphProto.Nodes {
		element, err := b.buildNode(node)
		if err != nil {
			return nil, fmt.Errorf("failed to build node %s: %w", node.Id, err)
		}
		elements[node.Id] = element
	}

	// Build connection string
	pipeline := b.buildPipeline(graphProto, elements)

	graph := &Graph{
		Pipeline: pipeline,
		Nodes:    graphProto.Nodes,
		logger:   b.logger,
	}

	b.logger.Info().Str("pipeline", pipeline).Msg("DSP graph built successfully")

	return graph, nil
}

// validateGraph checks that the graph is well-formed
func (b *Builder) validateGraph(graph *pb.DSPGraph) error {
	if len(graph.Nodes) == 0 {
		return fmt.Errorf("graph has no nodes")
	}

	// Check for duplicate node IDs
	seen := make(map[string]bool)
	for _, node := range graph.Nodes {
		if seen[node.Id] {
			return fmt.Errorf("duplicate node ID: %s", node.Id)
		}
		seen[node.Id] = true
	}

	// Validate connections reference existing nodes
	for _, conn := range graph.Connections {
		if !seen[conn.FromNode] {
			return fmt.Errorf("connection references unknown node: %s", conn.FromNode)
		}
		if !seen[conn.ToNode] {
			return fmt.Errorf("connection references unknown node: %s", conn.ToNode)
		}
	}

	return nil
}

// buildNode converts a DSP node proto into a GStreamer element string
func (b *Builder) buildNode(node *pb.DSPNode) (string, error) {
	switch node.Type {
	case pb.NodeType_NODE_TYPE_INPUT:
		return b.buildInputNode(node)
	case pb.NodeType_NODE_TYPE_OUTPUT:
		return b.buildOutputNode(node)
	case pb.NodeType_NODE_TYPE_LOUDNESS_NORMALIZE:
		return b.buildLoudnessNode(node)
	case pb.NodeType_NODE_TYPE_AGC:
		return b.buildAGCNode(node)
	case pb.NodeType_NODE_TYPE_COMPRESSOR:
		return b.buildCompressorNode(node)
	case pb.NodeType_NODE_TYPE_LIMITER:
		return b.buildLimiterNode(node)
	case pb.NodeType_NODE_TYPE_EQUALIZER:
		return b.buildEqualizerNode(node)
	case pb.NodeType_NODE_TYPE_GATE:
		return b.buildGateNode(node)
	case pb.NodeType_NODE_TYPE_SILENCE_DETECTOR:
		return b.buildSilenceDetectorNode(node)
	case pb.NodeType_NODE_TYPE_LEVEL_METER:
		return b.buildLevelMeterNode(node)
	case pb.NodeType_NODE_TYPE_MIX:
		return b.buildMixNode(node)
	case pb.NodeType_NODE_TYPE_DUCK:
		return b.buildDuckNode(node)
	default:
		return "", fmt.Errorf("unsupported node type: %v", node.Type)
	}
}

// buildInputNode creates an input element
func (b *Builder) buildInputNode(node *pb.DSPNode) (string, error) {
	// Input nodes are typically handled separately in the media engine
	// This is a placeholder for graph compilation
	return "identity", nil
}

// buildOutputNode creates an output element
func (b *Builder) buildOutputNode(node *pb.DSPNode) (string, error) {
	// Output nodes are typically handled separately in the media engine
	// This is a placeholder for graph compilation
	return "identity", nil
}

// buildLoudnessNode creates a loudness normalization element (EBU R128)
func (b *Builder) buildLoudnessNode(node *pb.DSPNode) (string, error) {
	// Use rgvolume for ReplayGain/loudness normalization
	targetLUFS := getParamFloat(node.Params, "target_lufs", -23.0)

	// GStreamer rgvolume doesn't directly support LUFS targets, but we can approximate
	// using pre-amp. EBU R128 target is typically -23 LUFS
	// rgvolume uses ReplayGain which targets -18 LUFS by default
	// We'll use a combination of rgvolume and volume elements

	element := fmt.Sprintf("rgvolume pre-amp=%.2f ! volume volume=%.2f",
		targetLUFS+18.0, // Adjust from -18 LUFS to target
		1.0,
	)

	return element, nil
}

// buildAGCNode creates an AGC (automatic gain control) element
func (b *Builder) buildAGCNode(node *pb.DSPNode) (string, error) {
	_ = getParamFloat(node.Params, "target_level", -20.0) // TODO: implement target level tracking
	maxGain := getParamFloat(node.Params, "max_gain", 12.0)

	// Use audioamplify with dynamic adjustment
	// In a real implementation, this would need custom logic or a plugin
	element := fmt.Sprintf("audioamplify amplification=%.2f clipping-method=0", maxGain)

	return element, nil
}

// buildCompressorNode creates a dynamics compressor element
func (b *Builder) buildCompressorNode(node *pb.DSPNode) (string, error) {
	threshold := getParamFloat(node.Params, "threshold", -20.0)
	ratio := getParamFloat(node.Params, "ratio", 4.0)
	attack := getParamFloat(node.Params, "attack_ms", 5.0)
	release := getParamFloat(node.Params, "release_ms", 50.0)

	// Use ladspa compressor plugin (sc4 is a common one)
	// Format: ladspa-sc4 attack=X release=Y threshold=Z ratio=W
	element := fmt.Sprintf("ladspa-sc4 attack=%.2f release=%.2f threshold=%.2f ratio=%.2f",
		attack, release, threshold, ratio)

	return element, nil
}

// buildLimiterNode creates a limiter element
func (b *Builder) buildLimiterNode(node *pb.DSPNode) (string, error) {
	threshold := getParamFloat(node.Params, "threshold", -1.0)
	_ = getParamFloat(node.Params, "release_ms", 10.0) // TODO: add release time support

	// Use audiodynamic as a limiter (mode=3 is hard-limit mode)
	element := fmt.Sprintf("audiodynamic mode=3 threshold=%.2f", threshold)

	return element, nil
}

// buildEqualizerNode creates an equalizer element
func (b *Builder) buildEqualizerNode(node *pb.DSPNode) (string, error) {
	bands := getParam(node.Params, "bands", "10")

	// Use equalizer-nbands for parametric EQ
	element := fmt.Sprintf("equalizer-nbands num-bands=%s", bands)

	// Apply band gains if specified (format: "band0=+3,band1=-2,...")
	if gains := getParam(node.Params, "gains", ""); gains != "" {
		for _, bandGain := range strings.Split(gains, ",") {
			element += " " + bandGain
		}
	}

	return element, nil
}

// buildGateNode creates a noise gate element
func (b *Builder) buildGateNode(node *pb.DSPNode) (string, error) {
	threshold := getParamFloat(node.Params, "threshold", -60.0)

	// Use audiodynamic as a gate (mode=1 is gate mode)
	element := fmt.Sprintf("audiodynamic mode=1 threshold=%.2f", threshold)

	return element, nil
}

// buildSilenceDetectorNode creates a silence detection element
func (b *Builder) buildSilenceDetectorNode(node *pb.DSPNode) (string, error) {
	threshold := getParamFloat(node.Params, "threshold", -50.0)
	minDurationMs := getParamInt(node.Params, "min_duration_ms", 2000)

	// Use silencedetect element
	element := fmt.Sprintf("silencedetect threshold=%.2f minimum-silence-time=%d",
		threshold, minDurationMs*1000000) // Convert ms to nanoseconds

	return element, nil
}

// buildLevelMeterNode creates a level metering element
func (b *Builder) buildLevelMeterNode(node *pb.DSPNode) (string, error) {
	intervalMs := getParamInt(node.Params, "interval_ms", 100)

	// Use level element for audio level metering
	element := fmt.Sprintf("level interval=%d message=true", intervalMs*1000000) // Convert ms to ns

	return element, nil
}

// buildMixNode creates a mixer element
func (b *Builder) buildMixNode(node *pb.DSPNode) (string, error) {
	// Use audiomixer for mixing multiple audio streams
	element := "audiomixer"

	return element, nil
}

// buildDuckNode creates an audio ducking element
func (b *Builder) buildDuckNode(node *pb.DSPNode) (string, error) {
	_ = getParamFloat(node.Params, "threshold", -20.0)   // TODO: implement ducking threshold
	_ = getParamFloat(node.Params, "reduction_db", -12.0) // TODO: implement ducking reduction

	// Audio ducking requires custom logic to reduce volume when a trigger signal is present
	// This would typically be implemented as a custom element or with audiomixer
	element := fmt.Sprintf("volume volume=%.2f", 1.0)

	return element, nil
}

// buildPipeline constructs the full GStreamer pipeline string
func (b *Builder) buildPipeline(graph *pb.DSPGraph, elements map[string]string) string {
	// Create a simple linear pipeline for now
	// In a more complex implementation, this would handle arbitrary graphs

	var pipeline strings.Builder

	// Build connection map
	connections := make(map[string]string)
	for _, conn := range graph.Connections {
		connections[conn.FromNode] = conn.ToNode
	}

	// Find input node
	var currentNode *pb.DSPNode
	for _, node := range graph.Nodes {
		if node.Type == pb.NodeType_NODE_TYPE_INPUT {
			currentNode = node
			break
		}
	}

	if currentNode == nil {
		// No input node, just chain all nodes
		if len(graph.Nodes) > 0 {
			currentNode = graph.Nodes[0]
		}
	}

	// Build linear chain following connections
	visited := make(map[string]bool)
	for currentNode != nil {
		if visited[currentNode.Id] {
			break // Avoid cycles
		}
		visited[currentNode.Id] = true

		element := elements[currentNode.Id]
		if pipeline.Len() > 0 {
			pipeline.WriteString(" ! ")
		}
		pipeline.WriteString(element)

		// Find next node
		nextNodeID, hasNext := connections[currentNode.Id]
		if !hasNext {
			break
		}

		var nextNode *pb.DSPNode
		for _, node := range graph.Nodes {
			if node.Id == nextNodeID {
				nextNode = node
				break
			}
		}
		currentNode = nextNode
	}

	return pipeline.String()
}

// Helper functions for parameter extraction

func getParam(params map[string]string, key, defaultValue string) string {
	if val, ok := params[key]; ok {
		return val
	}
	return defaultValue
}

func getParamFloat(params map[string]string, key string, defaultValue float64) float64 {
	if val, ok := params[key]; ok {
		var f float64
		if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
			return f
		}
	}
	return defaultValue
}

func getParamInt(params map[string]string, key string, defaultValue int) int {
	if val, ok := params[key]; ok {
		var i int
		if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
			return i
		}
	}
	return defaultValue
}
