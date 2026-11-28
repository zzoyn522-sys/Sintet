using System;
using System.Collections.Generic;

namespace Sintet {
 internal static class ReleaseLabel {
  public static bool IsBeta(Dictionary<string,object> payload) {
   object value;
   return payload.TryGetValue("beta",out value)&&value is bool&&(bool)value||
    payload.TryGetValue("publicBeta",out value)&&value is bool&&(bool)value;
  }
  public static string FromManifest(Dictionary<string,object> payload, string build, bool test, bool beta) {
   object raw;
   var display=payload.TryGetValue("displayVersion",out raw)?raw as string:null;
   Version version;
   if(Version.TryParse(display,out version)&&version.ToString()==display){
    if(version.Revision<0&&version.Build>=0&&
      (test?version.Major==0&&version!=new Version(0,0,0):version.Major==1&&version.Minor==1)&&
      (test||version.Build<=Int32.MaxValue/2)&&
      build==new Version(2,test?1:0,version.Minor,test?version.Build:version.Build*2+(beta?0:1)).ToString()){
     return display+(test?" TEST":beta?" BETA":"");
    }
    if(!test&&!beta&&version.Revision>=0&&version!=new Version(0,0,0,0))return display;
   }
   if(test){
    var parts=build.Split('.');
    return String.Join(".",parts,0,Math.Min(3,parts.Length))+" TEST";
   }
   return build+(beta?" BETA":"");
  }
 }
}
