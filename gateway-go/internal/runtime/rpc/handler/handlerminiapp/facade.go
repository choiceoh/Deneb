// facade.go re-exports the files, knowledge, and schedule leaf packages'
// public surface under handlerminiapp so server call sites import only the
// parent package. Module symbols live in handlerwire (import cycle + Methods
// name clash). Every alias is a straight re-export; no adapters.
package handlerminiapp

import (
	minifiles "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/files"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	minischedule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/schedule"
)

// --- files re-exports (miniapp.files.*) ---

type (
	FilesBrowseDeps = minifiles.FilesBrowseDeps
)

var FilesBrowseMethods = minifiles.FilesBrowseMethods

// --- knowledge re-exports (miniapp.memory/notebook/people/search/topicdocs.*) ---

type (
	MemoryDeps     = miniknowledge.MemoryDeps
	MemorySearcher = miniknowledge.MemorySearcher
	NotebookDeps   = miniknowledge.NotebookDeps
	PeopleClient   = miniknowledge.PeopleClient
	PeopleDeps     = miniknowledge.PeopleDeps
	SearchDeps     = miniknowledge.SearchDeps
	TopicDocsDeps  = miniknowledge.TopicDocsDeps
)

var (
	MemoryMethods    = miniknowledge.MemoryMethods
	NotebookMethods  = miniknowledge.NotebookMethods
	PeopleMethods    = miniknowledge.PeopleMethods
	SearchMethods    = miniknowledge.SearchMethods
	TopicDocsMethods = miniknowledge.TopicDocsMethods
)

// --- schedule re-exports (miniapp.calendar/todo/crons.*) ---

type (
	CalProposals   = minischedule.CalProposals
	CalendarClient = minischedule.CalendarClient
	CalendarDeps   = minischedule.CalendarDeps
	CronService    = minischedule.CronService
	CronsDeps      = minischedule.CronsDeps
	LocalCalendar  = minischedule.LocalCalendar
	LocalTodos     = minischedule.LocalTodos
	TodoDeps       = minischedule.TodoDeps
)

var (
	CalendarMethods = minischedule.CalendarMethods
	CronsMethods    = minischedule.CronsMethods
	TodoMethods     = minischedule.TodoMethods
)
