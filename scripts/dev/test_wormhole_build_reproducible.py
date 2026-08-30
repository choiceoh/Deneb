"""The wormhole build must stay reproducible, or deploy.sh restarts it forever.

deploy.sh restarts the router only when dist/wormhole's sha256 changed, because
"a restart drops in-flight LLM requests". That guard is defeated by anything the
linker stamps into the binary that varies between builds of identical source:
the -ldflags string (GO_LDFLAGS carries DENEB_BUILD_UNIX) and the VCS
revision/time (-buildvcs).

Measured 2026-08-30 before the fix: 41 wormhole restarts in 24h, each opening a
window of up to 22 minutes where sparkfleet-discovered models answered 404
"model does not exist" — because discovery only re-runs every ~7 minutes and its
first probe after a restart returns nothing.

These are static assertions on the recipe rather than a build-twice test: the
build is minutes long, and the recipe is where the regression would land.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

MAKEFILE = Path(__file__).resolve().parents[2] / "Makefile"


def wormhole_recipe() -> str:
    text = MAKEFILE.read_text(encoding="utf-8")
    match = re.search(r"^wormhole:\n((?:\t.*\n)+)", text, re.MULTILINE)
    assert match, "Makefile has no 'wormhole:' recipe"
    return match.group(1)


class WormholeBuildReproducibleTests(unittest.TestCase):
    def test_recipe_disables_vcs_stamping(self) -> None:
        self.assertIn(
            "-buildvcs=false",
            wormhole_recipe(),
            "VCS stamping puts the commit hash in the binary, so every deploy "
            "produces different bytes and deploy.sh restarts the router every time",
        )

    def test_recipe_does_not_use_the_timestamped_ldflags(self) -> None:
        recipe = wormhole_recipe()
        self.assertNotIn(
            "$(GO_LDFLAGS)",
            recipe,
            "GO_LDFLAGS embeds DENEB_BUILD_UNIX; the linker records the whole "
            "-ldflags string in build info, so identical source stops producing "
            "identical bytes",
        )
        self.assertIn("$(WORMHOLE_LDFLAGS)", recipe)

    def test_wormhole_ldflags_carry_no_varying_stamp(self) -> None:
        text = MAKEFILE.read_text(encoding="utf-8")
        match = re.search(r"^WORMHOLE_LDFLAGS *:?= *(.+)$", text, re.MULTILINE)
        self.assertIsNotNone(match, "WORMHOLE_LDFLAGS is not defined")
        value = match.group(1)
        for stamp in ("DENEB_BUILD_UNIX", "DENEB_VERSION", "BuildUnix"):
            self.assertNotIn(
                stamp,
                value,
                f"{stamp} varies between builds of identical source",
            )

    def test_deploy_still_gates_the_restart_on_the_checksum(self) -> None:
        # The reproducible build is only worth something because deploy.sh
        # compares checksums. If that comparison goes away, so does the reason
        # for this file.
        deploy = (MAKEFILE.parent / "scripts" / "deploy" / "deploy.sh").read_text(encoding="utf-8")
        self.assertIn("wormhole_new_sum", deploy)
        self.assertIn("wormhole_live_sum", deploy)
        self.assertIn('"$wormhole_new_sum" != "$wormhole_live_sum"', deploy)


if __name__ == "__main__":
    unittest.main()
