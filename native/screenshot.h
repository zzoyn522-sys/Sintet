#pragma once
#include <windows.h>
#include <memory>
#include <string>
#include <vector>

namespace SintetScreenshot {
struct Image {
 std::wstring filename;
 std::string label;
 UINT width=0,height=0;
 std::vector<BYTE> rgba;
};
struct Item {
 std::wstring filename;
 std::string label,date;
 unsigned long long modified=0;
};
using Gallery=std::vector<Item>;
inline constexpr size_t PageSize=18;

void Start();
void Stop();
void Request();
void Capture(HWND window);
const char* Notice();
void RefreshGallery();
std::shared_ptr<const Gallery> Catalog();
std::vector<std::shared_ptr<const Image>> Thumbnails(const std::shared_ptr<const Gallery>& catalog,size_t page);
void OpenImage(const std::wstring& filename);
void CopyImage(HWND window,const std::wstring& filename);
std::shared_ptr<const Image> Selected();
}
