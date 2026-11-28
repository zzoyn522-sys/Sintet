#pragma once

#include <windows.h>
#include <gl/GL.h>
#include <algorithm>
#include <array>
#include <cmath>
#include "overlay_style.h"

namespace SintetAcrylic {

// The overlay owns a separate WGL compatibility context. These passes never
// bind Minecraft's framebuffer objects or sample its internal render targets.
class Backdrop {
 static constexpr GLenum framebuffer = 0x8D40;
 static constexpr GLenum colorAttachment = 0x8CE0;
 static constexpr GLenum framebufferComplete = 0x8CD5;
 static constexpr GLenum clampToEdge = 0x812F;
 static constexpr int maxRadius = 72;
 static constexpr int maxTaps = 1 + maxRadius / 2;

 template<typename T> static T load(const char* name) {
  const auto address = wglGetProcAddress(name);
  const auto value = reinterpret_cast<INT_PTR>(address);
  return value > 3 ? reinterpret_cast<T>(address) : nullptr;
 }

 GLuint (APIENTRY *createShader)(GLenum) = nullptr;
 void (APIENTRY *shaderSource)(GLuint, GLsizei, const char* const*, const GLint*) = nullptr;
 void (APIENTRY *compileShader)(GLuint) = nullptr;
 void (APIENTRY *getShaderiv)(GLuint, GLenum, GLint*) = nullptr;
 void (APIENTRY *deleteShader)(GLuint) = nullptr;
 GLuint (APIENTRY *createProgram)() = nullptr;
 void (APIENTRY *attachShader)(GLuint, GLuint) = nullptr;
 void (APIENTRY *linkProgram)(GLuint) = nullptr;
 void (APIENTRY *getProgramiv)(GLuint, GLenum, GLint*) = nullptr;
 void (APIENTRY *deleteProgram)(GLuint) = nullptr;
 void (APIENTRY *useProgram)(GLuint) = nullptr;
 GLint (APIENTRY *getUniformLocation)(GLuint, const char*) = nullptr;
 void (APIENTRY *uniform1i)(GLint, GLint) = nullptr;
 void (APIENTRY *uniform2f)(GLint, GLfloat, GLfloat) = nullptr;
 void (APIENTRY *uniform2fv)(GLint, GLsizei, const GLfloat*) = nullptr;
 void (APIENTRY *genFramebuffers)(GLsizei, GLuint*) = nullptr;
 void (APIENTRY *deleteFramebuffers)(GLsizei, const GLuint*) = nullptr;
 void (APIENTRY *bindFramebuffer)(GLenum, GLuint) = nullptr;
 void (APIENTRY *framebufferTexture2D)(GLenum, GLenum, GLenum, GLuint, GLint) = nullptr;
 GLenum (APIENTRY *checkFramebufferStatus)(GLenum) = nullptr;

 std::array<GLuint, 2> textures{}, targets{};
 GLuint program = 0;
 GLint direction = -1, kernel = -1, tapCount = -1;
 int width = 0, height = 0;
 float lastRadius = 0;
 bool initialized = false;

 bool initialize() {
#define SINTET_GL_LOAD(member, name) member = load<decltype(member)>(#name); if (!member) return false
  SINTET_GL_LOAD(createShader, glCreateShader);
  SINTET_GL_LOAD(shaderSource, glShaderSource);
  SINTET_GL_LOAD(compileShader, glCompileShader);
  SINTET_GL_LOAD(getShaderiv, glGetShaderiv);
  SINTET_GL_LOAD(deleteShader, glDeleteShader);
  SINTET_GL_LOAD(createProgram, glCreateProgram);
  SINTET_GL_LOAD(attachShader, glAttachShader);
  SINTET_GL_LOAD(linkProgram, glLinkProgram);
  SINTET_GL_LOAD(getProgramiv, glGetProgramiv);
  SINTET_GL_LOAD(deleteProgram, glDeleteProgram);
  SINTET_GL_LOAD(useProgram, glUseProgram);
  SINTET_GL_LOAD(getUniformLocation, glGetUniformLocation);
  SINTET_GL_LOAD(uniform1i, glUniform1i);
  SINTET_GL_LOAD(uniform2f, glUniform2f);
  SINTET_GL_LOAD(uniform2fv, glUniform2fv);
  SINTET_GL_LOAD(genFramebuffers, glGenFramebuffers);
  SINTET_GL_LOAD(deleteFramebuffers, glDeleteFramebuffers);
  SINTET_GL_LOAD(bindFramebuffer, glBindFramebuffer);
  SINTET_GL_LOAD(framebufferTexture2D, glFramebufferTexture2D);
  SINTET_GL_LOAD(checkFramebufferStatus, glCheckFramebufferStatus);
#undef SINTET_GL_LOAD

  const char* vertex = R"GLSL(#version 110
varying vec2 uv;
void main() {
 uv = gl_MultiTexCoord0.st;
 gl_Position = gl_Vertex;
}
)GLSL";
  const char* fragment = R"GLSL(#version 110
uniform sampler2D scene;
uniform vec2 direction;
uniform int tapCount;
uniform vec2 kernel[37];
varying vec2 uv;
void main() {
 vec3 color = texture2D(scene, uv).rgb * kernel[0].y;
 for (int i = 1; i < tapCount; ++i) {
  vec2 offset = direction * kernel[i].x;
  color += kernel[i].y * (texture2D(scene, uv + offset).rgb + texture2D(scene, uv - offset).rgb);
 }
 gl_FragColor = vec4(color, 1.0);
}
)GLSL";
  const GLuint shaders[] = {compile(0x8B31, vertex), compile(0x8B30, fragment)};
  GLuint linked = 0;
  if (shaders[0] && shaders[1]) {
   linked = createProgram();
   for (const GLuint shader : shaders) attachShader(linked, shader);
   linkProgram(linked);
   GLint success = 0;
   getProgramiv(linked, 0x8B82, &success);
   if (!success) { deleteProgram(linked); linked = 0; }
  }
  for (const GLuint shader : shaders) if (shader) deleteShader(shader);
  if (!linked) return false;

  program = linked;
  direction = getUniformLocation(program, "direction");
  kernel = getUniformLocation(program, "kernel[0]");
  tapCount = getUniformLocation(program, "tapCount");
  glGenTextures(static_cast<GLsizei>(textures.size()), textures.data());
  genFramebuffers(static_cast<GLsizei>(targets.size()), targets.data());
  return true;
 }

 GLuint compile(GLenum type, const char* source) {
  const GLuint shader = createShader(type);
  if (!shader) return 0;
  shaderSource(shader, 1, &source, nullptr);
  compileShader(shader);
  GLint success = 0;
  getShaderiv(shader, 0x8B81, &success);
  if (success) return shader;
  deleteShader(shader);
  return 0;
 }

 bool resize(int captureWidth, int captureHeight) {
  if (captureWidth == width && captureHeight == height) return true;
  width = height = 0;
  for (size_t i = 0; i < textures.size(); ++i) {
   glBindTexture(GL_TEXTURE_2D, textures[i]);
   glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
   glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
   glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, clampToEdge);
   glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, clampToEdge);
   glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA8, captureWidth, captureHeight, 0, GL_RGBA, GL_UNSIGNED_BYTE, nullptr);
   bindFramebuffer(framebuffer, targets[i]);
   framebufferTexture2D(framebuffer, colorAttachment, GL_TEXTURE_2D, textures[i], 0);
   if (checkFramebufferStatus(framebuffer) != framebufferComplete) return false;
  }
  width = captureWidth;
  height = captureHeight;
  return true;
 }

 void setKernel(float radius) {
  if (lastRadius == radius) return;
  lastRadius = radius;
  const float sigma = radius / 3.f;
  const int extent = static_cast<int>(std::ceil(radius));
  const auto weight = [&](int distance) {
   const float normalized = distance / sigma;
   return std::exp(-.5f * normalized * normalized);
  };
  // Linear filtering joins each pair of adjacent taps without skipping any pixels.
  std::array<float, maxTaps * 2> samples{};
  samples[1] = 1.f;
  int count = 1;
  float total = 1.f;
  for (int pixel = 1; pixel <= extent; pixel += 2) {
   const float first = weight(pixel), second = pixel < extent ? weight(pixel + 1) : 0.f;
   const float sum = first + second;
   if (sum < 0.000001f) break;
   samples[count * 2] = pixel + second / sum;
   samples[count * 2 + 1] = sum;
   total += 2 * sum;
   ++count;
  }
  for (int i = 0; i < count; ++i) samples[i * 2 + 1] /= total;
  uniform1i(tapCount, count);
  uniform2fv(kernel, count, samples.data());
 }

 void pass(size_t source, size_t destination, float dx, float dy) {
  bindFramebuffer(framebuffer, targets[destination]);
  glBindTexture(GL_TEXTURE_2D, textures[source]);
  uniform2f(direction, dx, dy);
  glBegin(GL_TRIANGLE_STRIP);
  glTexCoord2f(0, 0); glVertex2f(-1, -1);
  glTexCoord2f(1, 0); glVertex2f(1, -1);
  glTexCoord2f(0, 1); glVertex2f(-1, 1);
  glTexCoord2f(1, 1); glVertex2f(1, 1);
  glEnd();
 }

public:
 GLuint capture(int left, int bottom, int captureWidth, int captureHeight, float blur) {
  if (captureWidth <= 0 || captureHeight <= 0 || blur <= 0) return 0;
  if (!initialized) { initialized = true; if (!initialize()) return 0; }
  if (!program) return 0;
  const float radius = std::min(blur, static_cast<float>(maxRadius));
  if (!resize(captureWidth, captureHeight)) {
   bindFramebuffer(framebuffer, 0);
   glBindTexture(GL_TEXTURE_2D, 0);
   return 0;
  }
  bindFramebuffer(framebuffer, 0);
  glBindTexture(GL_TEXTURE_2D, textures[0]);
  glReadBuffer(GL_BACK);
  glCopyTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, left, bottom, width, height);

  glPushAttrib(GL_VIEWPORT_BIT | GL_ENABLE_BIT);
  glDisable(GL_BLEND);
  glDisable(GL_SCISSOR_TEST);
  glDisable(GL_DEPTH_TEST);
  glDisable(GL_CULL_FACE);
  glViewport(0, 0, width, height);
  useProgram(program);
  setKernel(radius);
  pass(0, 1, 1.f / width, 0.f);
  pass(1, 0, 0.f, 1.f / height);
  useProgram(0);
  bindFramebuffer(framebuffer, 0);
  glBindTexture(GL_TEXTURE_2D, 0);
  glPopAttrib();
  return textures[0];
 }

 void release() {
  if (program) {
   deleteFramebuffers(static_cast<GLsizei>(targets.size()), targets.data());
   glDeleteTextures(static_cast<GLsizei>(textures.size()), textures.data());
   deleteProgram(program);
  }
  *this = {};
 }
};

inline void DrawSurface(Backdrop& backdrop,ImDrawList* draw,ImVec2 position,ImVec2 size,float scale,float alpha,float rounding,float sceneDim,
                        const SintetOverlayStyle::Appearance& material=SintetOverlayStyle::style){
 const ImVec2 end(position.x+size.x,position.y+size.y);
 draw->PushClipRectFullScreen();
 if(material.acrylic&&material.opacity<1.f&&material.blur>0.f){
  const auto screen=ImGui::GetIO().DisplaySize;
  const float blur=material.blur*scale;
  const int padding=static_cast<int>(std::ceil(blur))+2;
  const int left=std::max(0,static_cast<int>(std::floor(position.x))-padding);
  const int top=std::max(0,static_cast<int>(std::floor(position.y))-padding);
  const int right=std::min(static_cast<int>(screen.x),static_cast<int>(std::ceil(end.x))+padding);
  const int bottom=std::min(static_cast<int>(screen.y),static_cast<int>(std::ceil(end.y))+padding);
  const int width=right-left,height=bottom-top;
  if(const GLuint texture=backdrop.capture(left,static_cast<int>(screen.y)-bottom,width,height,blur)){
   const ImVec2 uv0((position.x-left)/width,1.f-(position.y-top)/height);
   const ImVec2 uv1((end.x-left)/width,1.f-(end.y-top)/height);
   const float light=1.f-sceneDim;
   draw->AddImageRounded((ImTextureID)(intptr_t)texture,position,end,uv0,uv1,
    ImGui::ColorConvertFloat4ToU32(ImVec4(light,light,light,alpha)),rounding);
  }
 }
 auto tint=material.surface;
 tint.w=material.opacity;
 draw->AddRectFilled(position,end,ImGui::GetColorU32(tint),rounding);
 draw->PopClipRect();
}

} // namespace SintetAcrylic
