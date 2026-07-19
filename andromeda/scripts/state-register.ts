/**
 * state-register — cross-stage shared-state read/write map for one TS type.
 *
 * The Go twin (gateway-go/cmd/state-register) runs go/types; this runs the
 * TypeScript compiler's own TypeChecker — no regex/syntactic heuristics. For a
 * React context value type like WorkspaceCtx, whose fields are set in the
 * provider and read across dozens of panes, this is the blast radius a
 * call/import graph can't show.
 *
 * Precision: every PropertyAccessExpression whose receiver the checker resolves
 * to the target type (or an apparent subtype carrying the property) is counted.
 * write = assignment LHS (=, +=, ++/--, delete); everything else read.
 * Accesses the checker cannot resolve are counted, never guessed.
 *
 * Usage:
 *   node --experimental-strip-types scripts/state-register.ts \
 *     [--type src/workspaceContext.tsx#WorkspaceCtx] [--out ../docs/research/state-register-workstation.md]
 *
 * Dependency-free: uses the `typescript` devDependency already present.
 */
import ts from "typescript";
import { writeFileSync } from "node:fs";
import { resolve, relative } from "node:path";

interface Site {
  file: string;
  line: number;
  fn: string;
  write: boolean;
}

function parseArgs(argv: string[]) {
  let typeArg = "src/workspaceContext.tsx#WorkspaceCtx";
  let out = "";
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--type") typeArg = argv[++i];
    else if (argv[i] === "--out") out = argv[++i];
  }
  const hash = typeArg.lastIndexOf("#");
  if (hash < 0) {
    console.error("--type must be file.ts#TypeName");
    process.exit(2);
  }
  return { typeFile: typeArg.slice(0, hash), typeName: typeArg.slice(hash + 1), out };
}

function loadProgram(): ts.Program {
  const cfgPath =
    ts.findConfigFile(process.cwd(), ts.sys.fileExists, "tsconfig.app.json") ??
    ts.findConfigFile(process.cwd(), ts.sys.fileExists, "tsconfig.json");
  if (!cfgPath) throw new Error("no tsconfig found");
  const cfg = ts.readConfigFile(cfgPath, ts.sys.readFile);
  const parsed = ts.parseJsonConfigFileContent(cfg.config, ts.sys, process.cwd());
  return ts.createProgram({ rootNames: parsed.fileNames, options: parsed.options });
}

/** enclosing function/component name for a node, best-effort. */
function enclosingFn(node: ts.Node): string {
  for (let p: ts.Node | undefined = node; p; p = p.parent) {
    if (ts.isFunctionDeclaration(p) && p.name) return p.name.text;
    if (ts.isMethodDeclaration(p) && ts.isIdentifier(p.name)) return p.name.text;
    if (
      (ts.isVariableDeclaration(p) || ts.isPropertyAssignment(p)) &&
      ts.isIdentifier(p.name) &&
      p.initializer &&
      (ts.isArrowFunction(p.initializer) || ts.isFunctionExpression(p.initializer))
    ) {
      return p.name.text;
    }
  }
  return "";
}

function isWriteContext(access: ts.PropertyAccessExpression): boolean {
  const parent = access.parent;
  if (ts.isBinaryExpression(parent) && parent.left === access) {
    const op = parent.operatorToken.kind;
    if (
      op === ts.SyntaxKind.EqualsToken ||
      (op >= ts.SyntaxKind.FirstAssignment && op <= ts.SyntaxKind.LastAssignment)
    ) {
      return true;
    }
  }
  if (
    (ts.isPrefixUnaryExpression(parent) || ts.isPostfixUnaryExpression(parent)) &&
    (parent.operator === ts.SyntaxKind.PlusPlusToken || parent.operator === ts.SyntaxKind.MinusMinusToken)
  ) {
    return true;
  }
  if (ts.isDeleteExpression(parent)) return true;
  return false;
}

function run() {
  const { typeFile, typeName, out } = parseArgs(process.argv.slice(2));
  const program = loadProgram();
  const checker = program.getTypeChecker();

  // Resolve the target type declaration.
  const declFileAbs = resolve(process.cwd(), typeFile);
  const declSource = program.getSourceFile(declFileAbs);
  if (!declSource) throw new Error(`type file not in program: ${typeFile}`);
  let targetSymbol: ts.Symbol | undefined;
  const findDecl = (n: ts.Node) => {
    if ((ts.isInterfaceDeclaration(n) || ts.isTypeAliasDeclaration(n)) && n.name.text === typeName) {
      targetSymbol = checker.getSymbolAtLocation(n.name);
    }
    ts.forEachChild(n, findDecl);
  };
  findDecl(declSource);
  if (!targetSymbol) throw new Error(`type ${typeName} not found in ${typeFile}`);
  const targetType = checker.getDeclaredTypeOfSymbol(targetSymbol);
  const fieldSet = new Set(checker.getPropertiesOfType(targetType).map((p) => p.name));

  const sites = new Map<string, Site[]>();
  let unresolved = 0;

  const typeMatches = (t: ts.Type): boolean => {
    if (t === targetType) return true;
    if (t.aliasSymbol && t.aliasSymbol === targetSymbol) return true;
    if (t.symbol && t.symbol === targetSymbol) return true;
    // union/intersection: any constituent is the target
    if (t.isUnionOrIntersection()) return t.types.some(typeMatches);
    return false;
  };

  for (const sf of program.getSourceFiles()) {
    if (sf.isDeclarationFile) continue;
    if (!sf.fileName.includes("/src/")) continue;
    if (sf.fileName.includes(".test.") || sf.fileName.includes("/mocks/")) continue;

    const rel = relative(resolve(process.cwd(), ".."), sf.fileName);
    const record = (field: string, node: ts.Node, write: boolean) => {
      const { line } = sf.getLineAndCharacterOfPosition(node.getStart());
      const arr = sites.get(field) ?? [];
      arr.push({ file: rel, line: line + 1, fn: enclosingFn(node), write });
      sites.set(field, arr);
    };

    const visit = (node: ts.Node) => {
      // Direct access: useWorkspace().view, ws.setView(...)
      if (ts.isPropertyAccessExpression(node) && fieldSet.has(node.name.text)) {
        const recvType = checker.getTypeAtLocation(node.expression);
        if (recvType && typeMatches(recvType)) {
          record(node.name.text, node.name, isWriteContext(node));
        } else if (!recvType || recvType.flags & ts.TypeFlags.Any) {
          unresolved++;
        }
      }
      // Destructuring: const { view, setView } = useWorkspace() — the dominant
      // consumption pattern. A binding element off a target-typed initializer is
      // a read of that field (const bindings can't be written through).
      if (ts.isVariableDeclaration(node) && node.initializer && node.name && ts.isObjectBindingPattern(node.name)) {
        const initType = checker.getTypeAtLocation(node.initializer);
        if (initType && typeMatches(initType)) {
          for (const el of node.name.elements) {
            const prop = el.propertyName ?? el.name;
            if (ts.isIdentifier(prop) && fieldSet.has(prop.text)) {
              record(prop.text, prop, false);
            }
          }
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(sf);
  }

  const md = render(typeFile, typeName, sites, unresolved);
  if (out) {
    writeFileSync(resolve(process.cwd(), out), md);
    console.error(`wrote ${out}`);
  } else {
    process.stdout.write(md);
  }
}

function render(typeFile: string, typeName: string, sites: Map<string, Site[]>, unresolved: number): string {
  const b: string[] = [];
  b.push(`# State register — ${typeFile}#${typeName}\n`);
  b.push(
    "<!-- GENERATED by andromeda/scripts/state-register.ts — DO NOT EDIT. Regenerate: make state-register-ts -->\n",
  );
  b.push("이 표는 공유 상태(React 컨텍스트 값) 한 타입의 필드별 write/read 지점을 파일 경계 너머까지");
  b.push('펼친다 (Go state-register의 TS 짝). import/콜 그래프가 못 보여주는 "이 상태를 바꾸면 어디가');
  b.push('영향받나"의 블래스트 반경. TypeScript 타입체커 기반 — 리시버 타입을 컴파일러가 해석하는');
  b.push("그대로 계수한다.\n");

  const fields = [...sites.keys()].sort();
  for (const f of fields) {
    const ss = sites.get(f)!;
    const w = ss.filter((s) => s.write);
    const r = ss.filter((s) => !s.write);
    const files = new Set(ss.map((s) => s.file.replace(/\/[^/]+$/, "")));
    const cross = files.size > 1 ? ` · **크로스-파일 ${files.size}개 디렉토리**` : "";
    b.push(`## ${f} — write ${w.length} · read ${r.length}${cross}\n`);
    const emit = (label: string, arr: Site[]) => {
      if (arr.length === 0) return;
      arr.sort((a, c) => (a.file === c.file ? a.line - c.line : a.file < c.file ? -1 : 1));
      b.push(`${label}:\n`);
      for (const s of arr) b.push(`- \`${s.file}:${s.line}\` ${s.fn}`);
      b.push("");
    };
    emit("**writes**", w);
    emit("reads", r);
  }
  b.push("---\n");
  b.push(`타입체커가 해석하지 못한(any/미해결 리시버) 필드명 일치 접근: ${unresolved}건.`);
  return b.join("\n") + "\n";
}

run();
