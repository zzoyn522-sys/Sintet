#include "language.h"
#include <cstdio>

struct Sample {
 const char* locale;
 const char* greeting;
 const char* screenshot;
 const char* ping;
};

int main(int argc, char** argv) {
 if(argc!=2)return 2;
 const Sample samples[]={
  {"en","Game launched with Sintet","Screenshot saved and copied","ms"},
  {"ru","Игра запущена через Sintet","Снимок сохранён и скопирован","мс"},
  {"uk","Гру запущено через Sintet","Знімок збережено й скопійовано","мс"},
  {"pt","Jogo iniciado pelo Sintet","Captura guardada e copiada","ms"},
  {"es","Juego iniciado con Sintet","Captura guardada y copiada","ms"}
 };
 for(const auto& sample:samples){
  if(std::strcmp(argv[1],sample.locale)!=0)continue;
  if(std::strcmp(OverlayText("Игра запущена через Sintet"),sample.greeting)!=0||
     std::strcmp(OverlayText("Снимок сохранён и скопирован"),sample.screenshot)!=0||
     std::strcmp(OverlayText("мс"),sample.ping)!=0){
   std::fprintf(stderr,"Overlay translation mismatch: %s\n",sample.locale);
   return 1;
  }
  std::printf("PASS overlay locale %s\n",sample.locale);
  return 0;
 }
 return 2;
}

