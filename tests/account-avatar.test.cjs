const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const vm=require('node:vm');
class Element{
 constructor(){this.children=[];this.events={};this.value='';this._text='';this.open=false;this.hidden=false}
 set textContent(value){this._text=value;for(const child of this.children)child.parentNode=null;this.children=[]}
 get textContent(){return this._text}
 set src(value){this._src=value}get src(){return this._src}
 getAttribute(name){return name==='src'?this._src:null}
 querySelector(selector){return selector==='img'?this.children[0]||null:null}
 querySelectorAll(){return []}
 replaceChildren(child){for(const old of this.children)old.parentNode=null;this.children=child?[child]:[];if(child)child.parentNode=this;this._text=''}
 addEventListener(name,callback){this.events[name]=callback}
 showModal(){this.open=true}close(){this.open=false}
}
function setup(){const nodes=new Map();const get=selector=>{if(!nodes.has(selector))nodes.set(selector,new Element());return nodes.get(selector)};get('#profile-name').textContent='Adolo';const listeners={};const window={SintetNetwork:{offline:false},addEventListener(name,callback){listeners[name]=callback}};vm.runInNewContext(readFileSync('frontend/account.js','utf8'),{window,document:{querySelector:get,createElement:()=>new Element()},setInterval:()=>0,clearInterval:()=>{}});return {get,window,listeners}}
test('avatar persists after identity sync, modal opening and profile events; new avatar reaches both views',()=>{const {get,window,listeners}=setup();const profile={id:'account',username:'Adolo',avatarURL:'http://127.0.0.1:3033/avatar-first.png'};window.SintetAccount.setAccount(profile,'sintet');const modal=get('#account-avatar'),header=get('#avatar'),image=modal.querySelector('img');assert.equal(image.src,profile.avatarURL);assert.equal(header.querySelector('img').src,profile.avatarURL);get('#account-button').events.click();assert.equal(modal.querySelector('img'),image);listeners['sintet:profile']();assert.equal(modal.querySelector('img'),image);window.SintetAccount.setAccount({...profile,avatarURL:'http://127.0.0.1:3033/avatar-next.png'},'sintet');assert.equal(modal.querySelector('img').src,'http://127.0.0.1:3033/avatar-next.png');image.onerror();assert.ok(modal.querySelector('img'),'stale image error must not clear the new avatar');assert.equal(header.querySelector('img').src,modal.querySelector('img').src);assert.equal(get('#account-skin-description').textContent,'Скин из профиля Sintet ID')});
test('failed image and local/Ely profiles use the default avatar without keeping another account photo',()=>{const {get,window}=setup();const profile={username:'Adolo',avatarURL:'http://127.0.0.1:3033/avatar.png'};window.SintetAccount.setAccount(profile,'sintet');get('#account-avatar').querySelector('img').onerror();assert.match(get('#account-avatar').querySelector('img').src,/default-avatar/);get('#account-avatar').querySelector('img').onerror();assert.equal(get('#account-avatar').textContent,'A');window.SintetAccount.setAccount(profile,'ely');assert.match(get('#avatar').querySelector('img').src,/default-avatar/);assert.match(get('#account-avatar').querySelector('img').src,/default-avatar/);get('#profile-name').textContent='Player';window.SintetAccount.setAccount(null);assert.match(get('#avatar').querySelector('img').src,/default-avatar/);assert.match(get('#account-avatar').querySelector('img').src,/default-avatar/)});

test('a delayed cached avatar from the previous account cannot replace the selected profile',async()=>{
 const {get,window}=setup();
 let resolveFirst;
 window.go={main:{App:{ResolveAvatar:url=>url.endsWith('/first')?new Promise(resolve=>resolveFirst=resolve):Promise.resolve('/cached-avatars/second.png')}}};
 window.SintetAccount.setAccount({username:'First',avatarURL:'https://portal.test/first'},'sintet');
 window.SintetAccount.setAccount({username:'Second',avatarURL:'https://portal.test/second'},'sintet');
 await Promise.resolve();
 await Promise.resolve();
 resolveFirst('/cached-avatars/first.png');
 await Promise.resolve();
 await Promise.resolve();
 assert.equal(get('#account-avatar').querySelector('img').src,'/cached-avatars/second.png');
 assert.equal(get('#avatar').querySelector('img').src,'/cached-avatars/second.png');
});
