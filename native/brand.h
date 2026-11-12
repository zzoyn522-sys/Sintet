#pragma once

// Cubic outline of the flat Duo SVG, in tenths of its viewBox units.
enum { SintetMarkWidth = 80, SintetMarkHeight = 70 };

static BOOL draw_sintet_mark(HDC dc, int x, int y, int width) {
 static const POINT outline[] = {
  {270,130},{470,130},{509,130},{540,161},{540,200},{540,330},{410,330},
  {333,330},{270,393},{270,470},{270,590},{150,590},
  {111,590},{80,559},{80,520},{80,320},{80,220},{170,130},{270,130},
  {490,430},{810,430},{849,430},{880,461},{880,500},{880,650},
  {880,750},{800,830},{700,830},{440,830},{401,830},{370,799},{370,760},
  {370,550},{370,480},{420,430},{490,430},
  {480,590},{480,720},{610,720},{610,590},
  {670,530},{670,640},{780,640},{780,530},
 };
 static const BYTE commands[] = {
  PT_MOVETO,PT_LINETO,PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO,PT_LINETO,PT_LINETO,
  PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO,PT_LINETO,PT_LINETO,
  PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO,PT_LINETO,PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO|PT_CLOSEFIGURE,
  PT_MOVETO,PT_LINETO,PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO,PT_LINETO,
  PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO,PT_LINETO,PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO,
  PT_LINETO,PT_BEZIERTO,PT_BEZIERTO,PT_BEZIERTO|PT_CLOSEFIGURE,
  PT_MOVETO,PT_LINETO,PT_LINETO,PT_LINETO|PT_CLOSEFIGURE,
  PT_MOVETO,PT_LINETO,PT_LINETO,PT_LINETO|PT_CLOSEFIGURE,
 };
 POINT points[sizeof outline / sizeof *outline];
 int previousMode;
 BOOL drawn;
 for (size_t i = 0; i < sizeof outline / sizeof *outline; i++) {
  points[i].x = x + MulDiv(outline[i].x - 80, width, SintetMarkWidth * 10);
  points[i].y = y + MulDiv(outline[i].y - 130, width, SintetMarkWidth * 10);
 }
 if (!BeginPath(dc)) return FALSE;
 if (!PolyDraw(dc, points, commands, sizeof commands / sizeof *commands) || !EndPath(dc)) {
  AbortPath(dc);
  return FALSE;
 }
 previousMode = SetPolyFillMode(dc, ALTERNATE);
 drawn = FillPath(dc);
 if (previousMode) SetPolyFillMode(dc, previousMode);
 return drawn;
}
