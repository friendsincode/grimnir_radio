package live

import (
	"context"

	"github.com/rs/zerolog"
)

// Service handles live input authorization and events.
type Service struct {
	logger zerolog.Logger
}

// NewService creates live input service placeholder.
func NewService(logger zerolog.Logger) *Service {
	return &Service{logger: logger}
}

// AuthorizeSource validates live source credentials placeholder.
func (s *Service) AuthorizeSource(ctx context.Context, stationID, mountID, token string) (bool, error) {
	s.logger.Debug().Str("station", stationID).Str("mount", mountID).Msg("authorize source placeholder")
	return false, nil
}

// HandleConnect tracks live DJ connect event placeholder.
func (s *Service) HandleConnect(ctx context.Context, stationID, mountID, userID string) error {
	s.logger.Info().Str("station", stationID).Str("mount", mountID).Str("user", userID).Msg("live connect placeholder")
	return nil
}

// HandleDisconnect tracks live DJ disconnect event placeholder.
func (s *Service) HandleDisconnect(ctx context.Context, stationID, mountID, userID string) error {
	s.logger.Info().Str("station", stationID).Str("mount", mountID).Str("user", userID).Msg("live disconnect placeholder")
	return nil
}
