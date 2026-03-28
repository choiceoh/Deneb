// directives_compat.go — temporary re-exports from the autoreply/directives subpackage.
// TODO: Remove after all callers are updated to import autoreply/directives directly.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/directives"

// Type aliases
type InlineDirectives = directives.InlineDirectives
type DirectiveParseOptions = directives.DirectiveParseOptions
type DirectiveHandlingResult = directives.DirectiveHandlingResult
type DirectiveModelResolution = directives.DirectiveModelResolution
type DirectiveQueueChanges = directives.DirectiveQueueChanges
type DirectiveHandlingOptions = directives.DirectiveHandlingOptions
type ResolvedLevels = directives.ResolvedLevels
type DirectiveParams = directives.DirectiveParams
type FullInlineDirectives = directives.FullInlineDirectives
type FullDirectiveParseOptions = directives.FullDirectiveParseOptions
type ExecHost = directives.ExecHost
type ExecSecurity = directives.ExecSecurity
type ExecAsk = directives.ExecAsk
type ExecDirectiveParse = directives.ExecDirectiveParse

// Function re-exports
var ParseInlineDirectives = directives.ParseInlineDirectives
var IsDirectiveOnly = directives.IsDirectiveOnly
var SkipDirectiveArgPrefix = directives.SkipDirectiveArgPrefix
var TakeDirectiveToken = directives.TakeDirectiveToken
var HandleDirectives = directives.HandleDirectives
var PersistDirectives = directives.PersistDirectives
var ResolveCurrentDirectiveLevels = directives.ResolveCurrentDirectiveLevels
var IsFastLaneDirective = directives.IsFastLaneDirective
var BuildFastLaneReply = directives.BuildFastLaneReply
var ParseDirectiveParams = directives.ParseDirectiveParams
var ValidateQueueDirective = directives.ValidateQueueDirective
var ParseFullInlineDirectives = directives.ParseFullInlineDirectives
var IsFullDirectiveOnly = directives.IsFullDirectiveOnly
var NormalizeExecHost = directives.NormalizeExecHost
var NormalizeExecSecurity = directives.NormalizeExecSecurity
var NormalizeExecAsk = directives.NormalizeExecAsk
var ExtractExecDirective = directives.ExtractExecDirective
