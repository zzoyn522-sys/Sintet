#pragma once

// The launcher mark, in the same centered coordinates as the SVG.
typedef struct { int x, y; } SintetMarkPoint;
typedef struct { const SintetMarkPoint *points; int count; } SintetMarkContour;

static const SintetMarkPoint sintetMarkRoof[] = {
 {-121,-139}, {0,-210}, {121,-139}, {86,-141}, {0,-187}, {-86,-141},
};
static const SintetMarkPoint sintetMarkCube[] = {
 {-121,-143}, {-86,-141}, {-65,-137}, {-43,-133}, {-22,-132}, {0,-131},
 {22,-132}, {43,-133}, {65,-137}, {86,-141}, {121,-143}, {121,-38},
 {101,-28}, {101,-118}, {12,-69}, {12,43}, {123,-18}, {172,8}, {0,108},
 {-172,8}, {-123,-18}, {-12,43}, {-12,-69}, {-101,-118}, {-101,-28}, {-121,-38},
};
static const SintetMarkPoint sintetMarkBase[] = {
 {-259,76}, {-178,33}, {-178,62}, {0,157}, {178,62}, {178,33}, {259,76}, {0,210},
};
static const SintetMarkContour sintetMarkContours[] = {
 {sintetMarkRoof, sizeof sintetMarkRoof / sizeof *sintetMarkRoof},
 {sintetMarkCube, sizeof sintetMarkCube / sizeof *sintetMarkCube},
 {sintetMarkBase, sizeof sintetMarkBase / sizeof *sintetMarkBase},
};
