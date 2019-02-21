# pman

A utility for managing work and changes that span multiple source repositories.

# Overview

The `pman` utility (short for _Project Manager_) is a simple wrapper for `git` that coordinates cloning, pulling, branching, and committing work that spans multiple Git repositories.  It is very similar to the Android `repo` tool (in fact, it uses the same XML manifest format), but offers a little more flexibility for offline environments, and is intended as a general-purpose tool whose primary use cases lie outside of Android/AOSP development.