# Claude Code Security & Permissions

## Permission Model

### 4 Permission Modes

| Mode | Behavior |
|------|----------|
| `default` | Prompt user for approval on each action |
| `auto` | ML classifier auto-approves low-risk actions |
| `bypass` | Skip all permission checks |
| `yolo` | Deny all actions (read-only mode) |

### Risk Classification

Every tool action is classified:
- **LOW**: File reads, searches, status checks
- **MEDIUM**: File writes, edits, git operations
- **HIGH**: Destructive operations, network access, system commands

### YOLO Classifier

Fast ML-based auto-approval system:
- Analyzes conversation transcript
- Predicts risk level of proposed action
- Auto-approves if below threshold
- Falls back to user prompt for uncertain cases

### Permission Explainer

Separate LLM call that explains risks before user approval:
- Describes what the action will do
- Identifies potential consequences
- Helps user make informed decision
- Only triggered for MEDIUM/HIGH risk actions

### Protected Files

Hard-coded list of files that always require explicit approval:
- `.gitconfig`
- `.bashrc` / `.zshrc`
- `.mcp.json`
- `.claude.json`

### Path Traversal Protection

Defenses against malicious file paths:
- URL-encoded traversal detection (`%2e%2e%2f`)
- Unicode normalization attacks
- Case manipulation (Windows)
- Symlink resolution

---

## Anti-Distillation Measures

### Fake Tool Injection

When `anti_distillation: ['fake_tools']` is enabled:
1. API request includes the flag
2. Server silently injects decoy tool definitions into system prompt
3. Decoy tools look real but are non-functional
4. Purpose: poison competitor distillation attempts

Community assessment: "They would parse them out using simple regex" — effectiveness debated.

### Undercover Mode

Default behavior for external (non-Anthropic) repos.

#### Activation
- Auto-active unless internal repo confirmed
- Anthropic employees (`USER_TYPE === 'ant'`) get automatic activation
- No explicit off switch
- Conservative: "if not confident we're internal, stay undercover"

#### Blocked Content in Commits/PRs
- Animal codenames: Capybara, Tengu, etc.
- Unreleased model versions: opus-4-7, sonnet-4-8
- Internal repository names
- Slack channel names
- Co-authored-by attributions mentioning Anthropic

#### Prompt Text
> "You are operating UNDERCOVER... Your commit messages...
> MUST NOT contain ANY Anthropic-internal information.
> Do not blow your cover."

#### Irony
The entire undercover system was exposed in the leak itself.

---

## CYBER_RISK_INSTRUCTION

Safeguards team-owned security prompt:
- Owners: David Forsythe, Kyla Guru
- Header: `"DO NOT MODIFY WITHOUT SAFEGUARDS TEAM REVIEW"`
- Handles security-sensitive behavioral boundaries
- Separate from general behavioral instructions
- Treated as critical security surface

---

## Client Attestation

Mechanisms to verify the client is genuine Claude Code:
- Client-side attestation tokens
- Server validates client identity
- Prevents unauthorized API access via custom clients

---

## Attribution System

### Configurable Attribution

Users can configure commit attribution:
```json
{
  "attribution": {
    "commit": "",    // empty = no co-author line
    "pr": ""         // empty = no PR attribution
  }
}
```

### Default Behavior
- Co-authored-by line added to commits by default
- PR descriptions mention Claude Code usage
- Users can disable entirely via config

### Debate
- Reviewers: need signals about AI-authored code for review calibration
- Developers: tools shouldn't be immortalized in commit history
- Pragmatic view: code quality matters regardless of origin

---

## Behavioral Safety

### Frustration Detection

Regex-based patterns match user frustration:
- Pattern matching on message content
- When detected: more careful, apologetic, focused behavior
- Language-specific patterns (English-focused in leaked version)

### Destructive Action Guards

Before destructive operations:
1. Identify action as HIGH risk
2. Explain consequences to user
3. Require explicit confirmation
4. Log the decision

Examples:
- `git push --force`
- `rm -rf`
- `git reset --hard`
- Database operations
- File overwrites without backup

### Employee vs External Differentiation

Different behavioral guardrails based on user type:
> "Anthropic employees get stricter/more honest instructions than external users"

Employees receive:
- More direct feedback
- Stricter safety constraints
- Additional internal context
- Access to internal-only tools (ConfigTool, TungstenTool)
