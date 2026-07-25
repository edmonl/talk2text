# Vendored OpenCORE AMR-WB Decoder

This directory contains the AMR-WB decoder subset from OpenCORE AMR.

1. Upstream project: https://sourceforge.net/projects/opencore-amr/
2. Release: `0.1.6` (`v0.1.6`)
3. Source archive: https://downloads.sourceforge.net/opencore-amr/opencore-amr-0.1.6.tar.gz
4. Archive SHA-256: `483eb4061088e2b34b358e47540b5d495a96cd468e361050fae615b1809dc4a1`
5. License: Apache License 2.0; see `LICENSE`.
6. Patent notice: see `PATENTS`.

The imported code consists of:

1. `amrwb/wrapper.cpp`, renamed to `wrapper.c`.
2. `amrwb/dec_if.h`.
3. The AMR-WB decoder sources listed by `amrwb/Makefile.am`, excluding `decoder_amr_wb.cpp`. Source files were renamed from `.cpp` to `.c`.
4. Headers from the AMR-WB decoder `src` and `include` directories.
5. `pvgsmamrdecoderinterface.h` from the shared decoder include directory.
6. The three headers from the upstream `oscl` directory.

The source contents are otherwise unchanged. The extension changes select the C99 build mode supported by the upstream build configuration and avoid a C++ runtime dependency.

Project-owned integration code is limited to `cgo.go`. Update the vendored snapshot by starting from a clean copy of a pinned upstream release, reproducing the file selection above, and reviewing the resulting Git diff.
