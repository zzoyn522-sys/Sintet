#include <cstdio>
#include "vendor/imgui/imgui.h"
#include "layout.h"

struct Metrics {float listScroll=0,listMax=0,outerScroll=0,cardScroll=0;};
static Metrics frame(int rows,float wheel=0){
 ImGuiIO& io=ImGui::GetIO();io.AddMousePosEvent(90,180);
 if(wheel)io.AddMouseWheelEvent(0,wheel);
 ImGui::NewFrame();
 ImGui::SetNextWindowPos(ImVec2(0,0));ImGui::SetNextWindowSize(ImVec2(414,600));
 ImGui::Begin("drawer",nullptr,ImGuiWindowFlags_NoDecoration|ImGuiWindowFlags_NoScrollWithMouse);
 Metrics out;out.outerScroll=ImGui::GetScrollY();
 ImGui::BeginChild("card",ImVec2(0,70),ImGuiChildFlags_AlwaysUseWindowPadding,SintetLayout::FixedCard);
 ImGui::TextUnformatted("Player");ImGui::TextUnformatted("In game");out.cardScroll=ImGui::GetScrollY();ImGui::EndChild();
 ImGui::BeginChild("content",ImVec2(0,350),ImGuiChildFlags_AlwaysUseWindowPadding,SintetLayout::ScrollableContent);
 for(int i=0;i<rows;i++){ImGui::Text("Friend %d",i);ImGui::Dummy(ImVec2(0,35));}
 out.listScroll=ImGui::GetScrollY();out.listMax=ImGui::GetScrollMaxY();
 ImGui::EndChild();ImGui::End();ImGui::Render();return out;
}
int main(){
 if(SintetLayout::TransferSize(4500).find("КБ")==std::string::npos||SintetLayout::TransferSize(12).find("12 Б")==std::string::npos){fputs("Small pack sizes were rounded to zero MB\n",stderr);return 3;}
 ImGui::CreateContext();auto& io=ImGui::GetIO();io.DisplaySize=ImVec2(800,600);io.DeltaTime=1.f/60;io.IniFilename=nullptr;io.LogFilename=nullptr;
 unsigned char* pixels;int width,height;io.Fonts->GetTexDataAsRGBA32(&pixels,&width,&height);
 for(int i=0;i<4;i++)frame(30);
 frame(30,-3);Metrics scrolled;
 for(int i=0;i<4;i++)scrolled=frame(30);
 if(scrolled.listScroll<=0||scrolled.listMax<=0||scrolled.outerScroll!=0||scrolled.cardScroll!=0){fputs("Overflow list did not scroll independently\n",stderr);return 1;}
 Metrics empty;for(int i=0;i<6;i++)empty=frame(1);
 frame(1,-3);for(int i=0;i<3;i++)empty=frame(1);
 if(empty.listMax!=0||empty.listScroll!=0||empty.outerScroll!=0||empty.cardScroll!=0){fputs("Short list or fixed header unexpectedly scrolls\n",stderr);return 2;}
 ImGui::DestroyContext();puts("Overlay scrolling passed: overflow list scrolls; short list and header stay fixed");return 0;
}
