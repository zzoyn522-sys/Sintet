# Sintet Overlay

Minecraft loads the embedded DLL as a JVM native agent with -agentpath. Agent_OnLoad starts the renderer bootstrap. Once LWJGL loads glfw.dll or glfw3.dll, a MinHook detour on glfwSwapBuffers renders Dear ImGui using the existing OpenGL context. No game window is created in this path. The launcher settings preview still uses the native preview window.

Shift+Tab toggles the panel, Escape closes it. Cursor mode is saved and restored through GLFW. Input is handled by the Win32 ImGui backend while the panel is visible. OpenGL state is restored by the ImGui renderer, including the previous draw framebuffer. The panel starts visible in the first rendered game frame.

Supported target is Windows x64 with GLFW and OpenGL 3.2+. Vulkan replacement renderers are unsupported. The in-game overlay can be hidden in launcher settings.

Vendored dependencies are Dear ImGui v1.91.9b (MIT) and MinHook v1.3.4 (BSD), with licenses under vendor. Build the overlay with `native/build.bat` and follow the [launcher build instructions](../README.md#сборка-из-исходников). Components are embedded in the launcher and extracted into a SHA-256 versioned directory. The launcher checks bytes before loading and keeps a read lock during the game's lifetime. Existing users receive it with the next launcher distribution.

The overlay reads friends and lobby state and sends user actions through the launcher's local IPC. The launcher owns account sessions and the P2P connection.
