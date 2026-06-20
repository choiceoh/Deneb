package ai.deneb.deneb

import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.deneb.generated.OrgSaveOut
import ai.deneb.deneb.generated.OrgTreeOut
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Org-chart surface of [DenebGatewayClient] (`miniapp.org.*`). An extension so the
 * gateway client stays one facade while each RPC domain lives in its own file (same
 * split as [DenebClientDashboard] et al.).
 *
 * The org chart (조직도) is the MASTER source for the dashboard's part classification:
 * a node tagged with a lane becomes a "파트별 업무 현황" column, and its members /
 * keywords / companies seed that part's classification rules (gateway-side
 * org.DeriveRules). Editing the chart re-derives the dashboard grouping — there is no
 * separate rules file. The chart is a flat node list joined by parentId; the editor
 * sends the whole tree on save (a document-style replace), which the gateway
 * validates (unique ids, existing parents, no cycles, unique lane keys) before
 * persisting to {stateDir}/org.json.
 */

/**
 * Fetch the full org chart (`miniapp.org.get`, no params). A missing file yields an
 * empty tree (the operator builds it); a corrupt file surfaces as a fetch failure.
 * Returns null on a fetch failure so the screen tells a real "empty chart" from a
 * network error instead of spinning forever (mirrors [fetchDashboardLanes]).
 */
suspend fun DenebGatewayClient.fetchOrg(): OrgTreeOut? = callRpc<OrgTreeOut>("miniapp.org.get", buildJsonObject {})

/**
 * Persist an edited chart (`miniapp.org.save`). The whole node list is sent as the
 * request body ({nodes: [...]}, the OrgTreeOut shape the gateway decodes params as)
 * so a save replaces the chart wholesale. Returns the gateway error message on
 * failure (e.g. a validation rejection: missing parent / cycle / duplicate lane) so
 * the editor can show the exact reason, or null on success.
 *
 * `nodes` is built explicitly (rather than encoding the whole OrgTreeOut) so the key
 * is always present even for an empty chart — the shared jsonCodec omits default
 * values, and an empty list is the OrgTreeOut default.
 */
suspend fun DenebGatewayClient.saveOrg(nodes: List<OrgNodeOut>): String? = rpcWrite(
    "miniapp.org.save",
    buildJsonObject {
        put("nodes", jsonCodec.encodeToJsonElement(ListSerializer(OrgNodeOut.serializer()), nodes))
    },
)

/**
 * Convenience: save and parse the ack into the typed [OrgSaveOut] (saved / nodeCount
 * / hasLanes). Returns null on any failure so callers that want the ack detail can
 * fall back to a generic error; most callers use [saveOrg] (error-string) directly.
 */
suspend fun DenebGatewayClient.saveOrgAck(nodes: List<OrgNodeOut>): OrgSaveOut? = callRpc<OrgSaveOut>(
    "miniapp.org.save",
    buildJsonObject {
        put("nodes", jsonCodec.encodeToJsonElement(ListSerializer(OrgNodeOut.serializer()), nodes))
    },
)
