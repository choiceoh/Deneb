# Third-Party Licenses

This project includes the following third-party binaries in `androidApp/src/main/jniLibs/`:

## PRoot (Termux fork)

- **Files:** `libproot.so`, `libproot-loader.so`, `libproot-loader32.so`
- **Source:** https://github.com/termux/proot
- **License:** GPL-2.0
- **Copyright:** Copyright (C) PRoot developers

PRoot is a user-space implementation of chroot, mount --bind, and binfmt_misc. It is used to run an Alpine Linux environment inside the Android app without requiring root access. PRoot is executed as a separate process and is not linked into the application code.

The full GPL-2.0 license text is available at: https://www.gnu.org/licenses/old-licenses/gpl-2.0.html

## talloc

- **Files:** `libtalloc.so`
- **Source:** https://talloc.samba.org/
- **License:** LGPL-3.0
- **Copyright:** Copyright (C) Andrew Tridgell, Stefan Metzmacher, and contributors

talloc is a hierarchical memory allocator used as a dependency of PRoot. It is dynamically linked.

The full LGPL-3.0 license text is available at: https://www.gnu.org/licenses/lgpl-3.0.html

## Pretendard

- **Files:** `composeApp/src/commonMain/composeResources/font/pretendard_*.otf`
- **Source:** https://github.com/orioncactus/pretendard
- **License:** SIL Open Font License 1.1
- **Copyright:** Copyright (c) 2021 Kil Hyung-jin, with Reserved Font Name Pretendard

Pretendard is the bundled UI typeface, used app-wide for all non-monospace text. The static OTF weights (Light, Regular, Medium, SemiBold, Bold) are embedded as Compose resources and loaded at runtime.

The full SIL Open Font License 1.1 text is available at: https://openfontlicense.org/open-font-license-official-text/
