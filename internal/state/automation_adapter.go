package state

import (
	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func (s *AutomationStore) OpenAutomationJournal(runID, workflowHash string) (application.AutomationJournal, []domain.AutomationJournalEvent, error) {
	return OpenAutomationJournal(s.paths, runID, workflowHash)
}
