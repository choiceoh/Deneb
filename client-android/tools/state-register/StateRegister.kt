/*
 * state-register (Kotlin) — cross-file read/write map for one data-class state
 * type, resolved with the Kotlin K1 frontend's BindingContext (real types, not
 * PSI-syntactic guesses). The Go twin runs go/types, the TS twin runs tsc; this
 * runs kotlin-compiler-embeddable so `state.field` is only counted when the
 * checker proves `state: <target>`.
 *
 * A Kotlin data class is immutable (val), so "writes" are `.copy(field = …)`
 * arguments — the places a change to the state actually originates — and
 * everything else is a read.
 *
 * Dependency-free w.r.t. the project: the compiler jars come from the gradle
 * cache, passed on the classpath; nothing is added to the KMP build graph.
 */
import org.jetbrains.kotlin.cli.common.CLIConfigurationKeys
import org.jetbrains.kotlin.cli.common.messages.MessageCollector
import org.jetbrains.kotlin.cli.jvm.compiler.EnvironmentConfigFiles
import org.jetbrains.kotlin.cli.jvm.compiler.KotlinCoreEnvironment
import org.jetbrains.kotlin.cli.jvm.compiler.TopDownAnalyzerFacadeForJVM
import org.jetbrains.kotlin.cli.jvm.config.addJvmClasspathRoots
import org.jetbrains.kotlin.config.CompilerConfiguration
import org.jetbrains.kotlin.config.CommonConfigurationKeys
import org.jetbrains.kotlin.config.JVMConfigurationKeys
import org.jetbrains.kotlin.com.intellij.openapi.util.Disposer
import org.jetbrains.kotlin.com.intellij.psi.PsiManager
import org.jetbrains.kotlin.com.intellij.openapi.vfs.local.CoreLocalFileSystem
import org.jetbrains.kotlin.psi.*
import org.jetbrains.kotlin.resolve.BindingContext
import org.jetbrains.kotlin.resolve.calls.util.getResolvedCall
import org.jetbrains.kotlin.resolve.lazy.descriptors.LazyClassDescriptor
import org.jetbrains.kotlin.descriptors.ClassDescriptor
import org.jetbrains.kotlin.resolve.descriptorUtil.fqNameSafe
import org.jetbrains.kotlin.types.KotlinType
import java.io.File

data class Site(val file: String, val line: Int, val fn: String, val write: Boolean)

fun main(args: Array<String>) {
    var typeFqn = "ai.deneb.ui.chat.ChatUiState"
    var srcRoots = ""
    var repoRoot = "."
    var out = ""
    var i = 0
    while (i < args.size) {
        when (args[i]) {
            "--type" -> typeFqn = args[++i]
            "--src" -> srcRoots = args[++i]        // colon-separated source dirs
            "--repo" -> repoRoot = args[++i]
            "--out" -> out = args[++i]
        }
        i++
    }
    val typeSimple = typeFqn.substringAfterLast('.')

    val disposable = Disposer.newDisposable()
    val cfg = CompilerConfiguration().apply {
        put(CLIConfigurationKeys.MESSAGE_COLLECTOR_KEY, MessageCollector.NONE)
        put(CommonConfigurationKeys.MODULE_NAME, "state-register")
        // Compile classpath from CP_ANALYSIS env (gradle cache jars) so external
        // types resolve too; project-local types resolve regardless.
        val cp = System.getenv("CP_ANALYSIS") ?: ""
        val roots = cp.split(':').filter { it.isNotBlank() }.map { File(it) }
        addJvmClasspathRoots(roots)
    }
    val env = KotlinCoreEnvironment.createForProduction(
        disposable, cfg, EnvironmentConfigFiles.JVM_CONFIG_FILES,
    )

    val psiManager = PsiManager.getInstance(env.project)
    val lfs = CoreLocalFileSystem()
    val ktFiles = mutableListOf<KtFile>()
    for (root in srcRoots.split(':').filter { it.isNotBlank() }) {
        File(root).walkTopDown()
            .filter { it.isFile && it.extension == "kt" && !it.name.endsWith("Test.kt") }
            .forEach { f ->
                val vf = lfs.findFileByIoFile(f) ?: return@forEach
                (psiManager.findFile(vf) as? KtFile)?.let { ktFiles.add(it) }
            }
    }

    val result = TopDownAnalyzerFacadeForJVM.analyzeFilesWithJavaIntegration(
        env.project, ktFiles, org.jetbrains.kotlin.cli.jvm.compiler.NoScopeRecordCliBindingTrace(env.project),
        env.configuration, env::createPackagePartProvider,
    )
    val bc: BindingContext = result.bindingContext

    // Collect field names of the target from its ClassDescriptor.
    val fields = linkedSetOf<String>()
    for (kt in ktFiles) {
        kt.declarations.filterIsInstance<KtClass>().forEach { cls ->
            val desc = bc.get(BindingContext.CLASS, cls) as? ClassDescriptor ?: return@forEach
            if (desc.fqNameSafe.asString() == typeFqn) {
                cls.primaryConstructor?.valueParameters?.forEach { p -> p.name?.let { fields.add(it) } }
                cls.body?.properties?.forEach { p -> p.name?.let { fields.add(it) } }
            }
        }
    }
    if (fields.isEmpty()) { System.err.println("type $typeFqn not found / not resolved"); return }

    val sites = linkedMapOf<String, MutableList<Site>>()
    var unresolved = 0
    fun rel(f: File) = f.absolutePath.substringAfter(File(repoRoot).absolutePath + "/", f.absolutePath)
    fun enclosing(e: KtElement): String {
        var p: org.jetbrains.kotlin.com.intellij.psi.PsiElement? = e
        while (p != null) {
            when (p) {
                is KtNamedFunction -> return p.name ?: "<fn>"
                is KtProperty -> if (p.isTopLevel || p.isMember) return p.name ?: ""
            }
            p = p.parent
        }
        return ""
    }

    fun typeIsTarget(t: KotlinType?): Boolean {
        val d = t?.constructor?.declarationDescriptor as? ClassDescriptor ?: return false
        return d.fqNameSafe.asString() == typeFqn
    }

    for (kt in ktFiles) {
        val ioFile = File(kt.virtualFilePath)
        val doc = PsiManager.getInstance(env.project).let { kt.viewProvider.document }
        fun lineOf(e: KtElement) = (doc?.getLineNumber(e.textOffset) ?: -1) + 1

        kt.accept(object : KtTreeVisitorVoid() {
            // reads / writes via receiver.field
            override fun visitDotQualifiedExpression(expr: KtDotQualifiedExpression) {
                val sel = expr.selectorExpression
                if (sel is KtNameReferenceExpression && sel.getReferencedName() in fields) {
                    val recvType = bc.getType(expr.receiverExpression)
                    if (typeIsTarget(recvType)) {
                        sites.getOrPut(sel.getReferencedName()) { mutableListOf() }
                            .add(Site(rel(ioFile), lineOf(sel), enclosing(sel), false))
                    } else if (recvType == null) {
                        unresolved++
                    }
                }
                super.visitDotQualifiedExpression(expr)
            }
            // writes: target.copy(field = …) named args
            override fun visitCallExpression(call: KtCallExpression) {
                val callee = call.calleeExpression
                if (callee is KtNameReferenceExpression && callee.getReferencedName() == "copy") {
                    val parent = call.parent
                    val recvType = if (parent is KtDotQualifiedExpression)
                        bc.getType(parent.receiverExpression) else null
                    if (typeIsTarget(recvType)) {
                        call.valueArguments.forEach { arg ->
                            val name = arg.getArgumentName()?.asName?.asString()
                            if (name != null && name in fields) {
                                sites.getOrPut(name) { mutableListOf() }
                                    .add(Site(rel(ioFile), lineOf(call), enclosing(call), true))
                            }
                        }
                    }
                }
                super.visitCallExpression(call)
            }
        })
    }

    val md = render(typeFqn, typeSimple, sites, unresolved)
    if (out.isNotEmpty()) { File(out).writeText(md); System.err.println("wrote $out") }
    else print(md)
    Disposer.dispose(disposable)
}

fun render(fqn: String, simple: String, sites: Map<String, List<Site>>, unresolved: Int): String {
    val b = StringBuilder()
    b.append("# State register — $fqn\n\n")
    b.append("<!-- GENERATED by client-android/tools/state-register — DO NOT EDIT. Regenerate: make state-register-kt -->\n\n")
    b.append("이 표는 공유 상태(Compose data class) 한 타입의 필드별 write/read 지점을 파일 경계 너머까지\n")
    b.append("펼친다 (Go·TS state-register의 Kotlin 짝). data class는 불변(val)이라 write=`.copy(field=…)`\n")
    b.append("인자(변경이 실제 발원하는 곳), 그 외 접근은 read. Kotlin K1 프론트엔드 BindingContext 기반 —\n")
    b.append("리시버 타입을 컴파일러가 해석한 그대로 계수한다.\n\n")
    for (f in sites.keys.sorted()) {
        val ss = sites.getValue(f)
        val w = ss.filter { it.write }; val r = ss.filter { !it.write }
        val dirs = ss.map { it.file.substringBeforeLast('/') }.toSet()
        val cross = if (dirs.size > 1) " · **크로스-파일 ${dirs.size}개 디렉토리**" else ""
        b.append("## $f — write ${w.size} · read ${r.size}$cross\n\n")
        fun emit(label: String, arr: List<Site>) {
            if (arr.isEmpty()) return
            b.append("$label:\n\n")
            arr.sortedWith(compareBy({ it.file }, { it.line })).forEach {
                b.append("- `${it.file}:${it.line}` ${it.fn}\n")
            }
            b.append("\n")
        }
        emit("**writes**", w); emit("reads", r)
    }
    b.append("---\n\n타입체커가 해석하지 못한(리시버 미해결) 필드명 일치 접근: ${unresolved}건.\n")
    return b.toString()
}
