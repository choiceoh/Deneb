/**
 * Async security audit: workspace skill symlink escape and code safety checks.
 */
import fs from "node:fs/promises";
import path from "node:path";
import { listAgentWorkspaceDirs } from "../agents/workspace/workspace-dirs.js";
import type { DenebConfig } from "../config/config.js";
import type { SecurityAuditFinding } from "./audit-extra-shared.js";
import {
  listWorkspaceSkillMarkdownFiles,
  getCodeSafetySummary,
  formatCodeSafetyDetails,
  loadSkillsModule,
  MAX_WORKSPACE_SKILL_ESCAPE_DETAIL_ROWS,
  type CodeSafetySummaryCache,
} from "./audit-extra.async.helpers.js";
import { isPathInside } from "./scan-paths.js";

export async function collectWorkspaceSkillSymlinkEscapeFindings(params: {
  cfg: DenebConfig;
}): Promise<SecurityAuditFinding[]> {
  const findings: SecurityAuditFinding[] = [];
  const workspaceDirs = listAgentWorkspaceDirs(params.cfg);
  if (workspaceDirs.length === 0) {
    return findings;
  }

  const escapedSkillFiles: Array<{
    workspaceDir: string;
    skillFilePath: string;
    skillRealPath: string;
  }> = [];
  const seenSkillPaths = new Set<string>();

  for (const workspaceDir of workspaceDirs) {
    const workspacePath = path.resolve(workspaceDir);
    const workspaceRealPath = await fs.realpath(workspacePath).catch(() => workspacePath);
    const skillFilePaths = await listWorkspaceSkillMarkdownFiles(workspacePath);

    for (const skillFilePath of skillFilePaths) {
      const canonicalSkillPath = path.resolve(skillFilePath);
      if (seenSkillPaths.has(canonicalSkillPath)) {
        continue;
      }
      seenSkillPaths.add(canonicalSkillPath);

      const skillRealPath = await fs.realpath(canonicalSkillPath).catch(() => null);
      if (!skillRealPath) {
        continue;
      }
      if (isPathInside(workspaceRealPath, skillRealPath)) {
        continue;
      }
      escapedSkillFiles.push({
        workspaceDir: workspacePath,
        skillFilePath: canonicalSkillPath,
        skillRealPath,
      });
    }
  }

  if (escapedSkillFiles.length === 0) {
    return findings;
  }

  findings.push({
    checkId: "skills.workspace.symlink_escape",
    severity: "warn",
    title: "Workspace skill files resolve outside the workspace root",
    detail:
      "Detected workspace `skills/**/SKILL.md` paths whose realpath escapes their workspace root:\n" +
      escapedSkillFiles
        .slice(0, MAX_WORKSPACE_SKILL_ESCAPE_DETAIL_ROWS)
        .map(
          (entry) =>
            `- workspace=${entry.workspaceDir}\n` +
            `  skill=${entry.skillFilePath}\n` +
            `  realpath=${entry.skillRealPath}`,
        )
        .join("\n") +
      (escapedSkillFiles.length > MAX_WORKSPACE_SKILL_ESCAPE_DETAIL_ROWS
        ? `\n- +${escapedSkillFiles.length - MAX_WORKSPACE_SKILL_ESCAPE_DETAIL_ROWS} more`
        : ""),
    remediation:
      "Keep workspace skills inside the workspace root (replace symlinked escapes with real in-workspace files), or move trusted shared skills to managed/bundled skill locations.",
  });

  return findings;
}

export async function collectInstalledSkillsCodeSafetyFindings(params: {
  cfg: DenebConfig;
  stateDir: string;
  summaryCache?: CodeSafetySummaryCache;
}): Promise<SecurityAuditFinding[]> {
  const findings: SecurityAuditFinding[] = [];
  const pluginExtensionsDir = path.join(params.stateDir, "extensions");
  const scannedSkillDirs = new Set<string>();
  const workspaceDirs = listAgentWorkspaceDirs(params.cfg);
  const { loadWorkspaceSkillEntries } = await loadSkillsModule();

  for (const workspaceDir of workspaceDirs) {
    const entries = loadWorkspaceSkillEntries(workspaceDir, { config: params.cfg });
    for (const entry of entries) {
      if (entry.skill.source === "deneb-bundled") {
        continue;
      }

      const skillDir = path.resolve(entry.skill.baseDir);
      if (isPathInside(pluginExtensionsDir, skillDir)) {
        // Plugin code is already covered by plugins.code_safety checks.
        continue;
      }
      if (scannedSkillDirs.has(skillDir)) {
        continue;
      }
      scannedSkillDirs.add(skillDir);

      const skillName = entry.skill.name;
      const summary = await getCodeSafetySummary({
        dirPath: skillDir,
        summaryCache: params.summaryCache,
      }).catch((err) => {
        findings.push({
          checkId: "skills.code_safety.scan_failed",
          severity: "warn",
          title: `Skill "${skillName}" code scan failed`,
          detail: `Static code scan could not complete for ${skillDir}: ${String(err)}`,
          remediation:
            "Check file permissions and skill layout, then rerun `deneb security audit --deep`.",
        });
        return null;
      });
      if (!summary) {
        continue;
      }

      if (summary.critical > 0) {
        const criticalFindings = summary.findings.filter(
          (finding) => finding.severity === "critical",
        );
        const details = formatCodeSafetyDetails(criticalFindings, skillDir);
        findings.push({
          checkId: "skills.code_safety",
          severity: "critical",
          title: `Skill "${skillName}" contains dangerous code patterns`,
          detail: `Found ${summary.critical} critical issue(s) in ${summary.scannedFiles} scanned file(s) under ${skillDir}:\n${details}`,
          remediation: `Review the skill source code before use. If untrusted, remove "${skillDir}".`,
        });
      } else if (summary.warn > 0) {
        const warnFindings = summary.findings.filter((finding) => finding.severity === "warn");
        const details = formatCodeSafetyDetails(warnFindings, skillDir);
        findings.push({
          checkId: "skills.code_safety",
          severity: "warn",
          title: `Skill "${skillName}" contains suspicious code patterns`,
          detail: `Found ${summary.warn} warning(s) in ${summary.scannedFiles} scanned file(s) under ${skillDir}:\n${details}`,
          remediation: "Review flagged lines to ensure the behavior is intentional and safe.",
        });
      }
    }
  }

  return findings;
}
