import{S as $t,i as qt,s as Tt,V as St,X as ce,W as Ct,j as o,d as $e,t as ke,a as ve,I as se,Z as Ne,_ as pt,C as Mt,$ as Lt,D as Ft,n as r,o as i,m as qe,u as s,A as b,v as p,c as Te,w,J as we,b as Ht,l as Se,p as Ot,L as Rt,H as fe}from"./index-DQaqjr2E.js";import{F as At}from"./FieldsQueryParam-po9OWZMy.js";function mt(a,e,t){const l=a.slice();return l[10]=e[t],l}function bt(a,e,t){const l=a.slice();return l[10]=e[t],l}function _t(a,e,t){const l=a.slice();return l[15]=e[t],l}function yt(a){let e;return{c(){e=s("p"),e.innerHTML="Requires superuser <code>Authorization:TOKEN</code> header",w(e,"class","txt-hint txt-sm txt-right")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function ht(a){let e,t,l,c,f,u,_,m,q,h,g,B,S,$,A,L,I,D,M,Q,F,T,y,H,ee,x,U,oe,K,X,Y;function ue(k,C){var N,W,R;return C&1&&(u=null),u==null&&(u=!!((R=(W=(N=k[0])==null?void 0:N.fields)==null?void 0:W.find(Yt))!=null&&R.required)),u?Bt:Pt}let te=ue(a,-1),E=te(a);function Z(k,C){var N,W,R;return C&1&&(I=null),I==null&&(I=!!((R=(W=(N=k[0])==null?void 0:N.fields)==null?void 0:W.find(Xt))!=null&&R.required)),I?Nt:Vt}let G=Z(a,-1),O=G(a);return{c(){e=s("tr"),e.innerHTML='<td colspan="3" class="txt-hint txt-bold">Auth specific fields</td>',t=p(),l=s("tr"),c=s("td"),f=s("div"),E.c(),_=p(),m=s("span"),m.textContent="email",q=p(),h=s("td"),h.innerHTML='<span class="label">String</span>',g=p(),B=s("td"),B.textContent="Auth record email address.",S=p(),$=s("tr"),A=s("td"),L=s("div"),O.c(),D=p(),M=s("span"),M.textContent="emailVisibility",Q=p(),F=s("td"),F.innerHTML='<span class="label">Boolean</span>',T=p(),y=s("td"),y.textContent="Whether to show/hide the auth record email when fetching the record data.",H=p(),ee=s("tr"),ee.innerHTML='<td><div class="inline-flex"><span class="label label-success">Required</span> <span>password</span></div></td> <td><span class="label">String</span></td> <td>Auth record password.</td>',x=p(),U=s("tr"),U.innerHTML='<td><div class="inline-flex"><span class="label label-success">Required</span> <span>passwordConfirm</span></div></td> <td><span class="label">String</span></td> <td>Auth record password confirmation.</td>',oe=p(),K=s("tr"),K.innerHTML=`<td><div class="inline-flex"><span class="label label-warning">Optional</span> <span>verified</span></div></td> <td><span class="label">Boolean</span></td> <td>Indicates whether the auth record is verified or not.
                    <br/>
                    This field can be set only by superusers or auth records with &quot;Manage&quot; access.</td>`,X=p(),Y=s("tr"),Y.innerHTML='<td colspan="3" class="txt-hint txt-bold">Other fields</td>',w(f,"class","inline-flex"),w(L,"class","inline-flex")},m(k,C){r(k,e,C),r(k,t,C),r(k,l,C),i(l,c),i(c,f),E.m(f,null),i(f,_),i(f,m),i(l,q),i(l,h),i(l,g),i(l,B),r(k,S,C),r(k,$,C),i($,A),i(A,L),O.m(L,null),i(L,D),i(L,M),i($,Q),i($,F),i($,T),i($,y),r(k,H,C),r(k,ee,C),r(k,x,C),r(k,U,C),r(k,oe,C),r(k,K,C),r(k,X,C),r(k,Y,C)},p(k,C){te!==(te=ue(k,C))&&(E.d(1),E=te(k),E&&(E.c(),E.m(f,_))),G!==(G=Z(k,C))&&(O.d(1),O=G(k),O&&(O.c(),O.m(L,D)))},d(k){k&&(o(e),o(t),o(l),o(S),o($),o(H),o(ee),o(x),o(U),o(oe),o(K),o(X),o(Y)),E.d(),O.d()}}}function Pt(a){let e;return{c(){e=s("span"),e.textContent="Optional",w(e,"class","label label-warning")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function Bt(a){let e;return{c(){e=s("span"),e.textContent="Required",w(e,"class","label label-success")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function Vt(a){let e;return{c(){e=s("span"),e.textContent="Optional",w(e,"class","label label-warning")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function Nt(a){let e;return{c(){e=s("span"),e.textContent="Required",w(e,"class","label label-success")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function jt(a){let e;return{c(){e=s("span"),e.textContent="Required",w(e,"class","label label-success")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function Jt(a){let e;return{c(){e=s("span"),e.textContent="Optional",w(e,"class","label label-warning")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function Dt(a){let e,t=a[15].maxSelect===1?"id":"ids",l,c;return{c(){e=b("Relation record "),l=b(t),c=b(".")},m(f,u){r(f,e,u),r(f,l,u),r(f,c,u)},p(f,u){u&32&&t!==(t=f[15].maxSelect===1?"id":"ids")&&se(l,t)},d(f){f&&(o(e),o(l),o(c))}}}function Et(a){let e,t,l,c,f,u,_,m,q;return{c(){e=b("File object."),t=s("br"),l=b(`
                        Set to empty value (`),c=s("code"),c.textContent="null",f=b(", "),u=s("code"),u.textContent='""',_=b(" or "),m=s("code"),m.textContent="[]",q=b(`) to delete
                        already uploaded file(s).`)},m(h,g){r(h,e,g),r(h,t,g),r(h,l,g),r(h,c,g),r(h,f,g),r(h,u,g),r(h,_,g),r(h,m,g),r(h,q,g)},p:fe,d(h){h&&(o(e),o(t),o(l),o(c),o(f),o(u),o(_),o(m),o(q))}}}function It(a){let e,t;return{c(){e=s("code"),e.textContent='{"lon":x,"lat":y}',t=b(" object.")},m(l,c){r(l,e,c),r(l,t,c)},p:fe,d(l){l&&(o(e),o(t))}}}function Ut(a){let e;return{c(){e=b("URL address.")},m(t,l){r(t,e,l)},p:fe,d(t){t&&o(e)}}}function zt(a){let e;return{c(){e=b("Email address.")},m(t,l){r(t,e,l)},p:fe,d(t){t&&o(e)}}}function Qt(a){let e;return{c(){e=b("JSON array or object.")},m(t,l){r(t,e,l)},p:fe,d(t){t&&o(e)}}}function Wt(a){let e;return{c(){e=b("Number value.")},m(t,l){r(t,e,l)},p:fe,d(t){t&&o(e)}}}function xt(a){let e,t,l=a[15].autogeneratePattern&&kt();return{c(){e=b(`Plain text value.
                        `),l&&l.c(),t=Rt()},m(c,f){r(c,e,f),l&&l.m(c,f),r(c,t,f)},p(c,f){c[15].autogeneratePattern?l||(l=kt(),l.c(),l.m(t.parentNode,t)):l&&(l.d(1),l=null)},d(c){c&&(o(e),o(t)),l&&l.d(c)}}}function kt(a){let e;return{c(){e=b("It is autogenerated if not set.")},m(t,l){r(t,e,l)},d(t){t&&o(e)}}}function vt(a,e){let t,l,c,f,u,_=e[15].name+"",m,q,h,g,B=we.getFieldValueType(e[15])+"",S,$,A,L;function I(y,H){return!y[15].required||y[15].type=="text"&&y[15].autogeneratePattern?Jt:jt}let D=I(e),M=D(e);function Q(y,H){if(y[15].type==="text")return xt;if(y[15].type==="number")return Wt;if(y[15].type==="json")return Qt;if(y[15].type==="email")return zt;if(y[15].type==="url")return Ut;if(y[15].type==="geoPoint")return It;if(y[15].type==="file")return Et;if(y[15].type==="relation")return Dt}let F=Q(e),T=F&&F(e);return{key:a,first:null,c(){t=s("tr"),l=s("td"),c=s("div"),M.c(),f=p(),u=s("span"),m=b(_),q=p(),h=s("td"),g=s("span"),S=b(B),$=p(),A=s("td"),T&&T.c(),L=p(),w(c,"class","inline-flex"),w(g,"class","label"),this.first=t},m(y,H){r(y,t,H),i(t,l),i(l,c),M.m(c,null),i(c,f),i(c,u),i(u,m),i(t,q),i(t,h),i(h,g),i(g,S),i(t,$),i(t,A),T&&T.m(A,null),i(t,L)},p(y,H){e=y,D!==(D=I(e))&&(M.d(1),M=D(e),M&&(M.c(),M.m(c,f))),H&32&&_!==(_=e[15].name+"")&&se(m,_),H&32&&B!==(B=we.getFieldValueType(e[15])+"")&&se(S,B),F===(F=Q(e))&&T?T.p(e,H):(T&&T.d(1),T=F&&F(e),T&&(T.c(),T.m(A,null)))},d(y){y&&o(t),M.d(),T&&T.d()}}}function wt(a,e){let t,l=e[10].code+"",c,f,u,_;function m(){return e[9](e[10])}return{key:a,first:null,c(){t=s("button"),c=b(l),f=p(),w(t,"class","tab-item"),Se(t,"active",e[2]===e[10].code),this.first=t},m(q,h){r(q,t,h),i(t,c),i(t,f),u||(_=Ot(t,"click",m),u=!0)},p(q,h){e=q,h&8&&l!==(l=e[10].code+"")&&se(c,l),h&12&&Se(t,"active",e[2]===e[10].code)},d(q){q&&o(t),u=!1,_()}}}function gt(a,e){let t,l,c,f;return l=new Ct({props:{content:e[10].body}}),{key:a,first:null,c(){t=s("div"),Te(l.$$.fragment),c=p(),w(t,"class","tab-item"),Se(t,"active",e[2]===e[10].code),this.first=t},m(u,_){r(u,t,_),qe(l,t,null),i(t,c),f=!0},p(u,_){e=u;const m={};_&8&&(m.content=e[10].body),l.$set(m),(!f||_&12)&&Se(t,"active",e[2]===e[10].code)},i(u){f||(ve(l.$$.fragment,u),f=!0)},o(u){ke(l.$$.fragment,u),f=!1},d(u){u&&o(t),$e(l)}}}function Kt(a){var at,st,ot,rt;let e,t,l=a[0].name+"",c,f,u,_,m,q,h,g=a[0].name+"",B,S,$,A,L,I,D,M,Q,F,T,y,H,ee,x,U,oe,K,X=a[0].name+"",Y,ue,te,E,Z,G,O,k,C,N,W,R=[],je=new Map,Me,pe,Le,le,Fe,Je,me,ne,He,De,Oe,Ee,P,Ie,re,Ue,ze,Qe,Re,We,Ae,xe,Ke,Xe,Pe,Ye,Ze,de,Be,be,Ve,ie,_e,z=[],Ge=new Map,et,ye,j=[],tt=new Map,ae;M=new St({props:{js:`
import Base from 'base';

const base = new Base('${a[4]}');

...

// example create data
const data = ${JSON.stringify(a[7](a[0]),null,4)};

const record = await base.collection('${(at=a[0])==null?void 0:at.name}').create(data);
`+(a[1]?`
// (optional) send an email verification request
await base.collection('${(st=a[0])==null?void 0:st.name}').requestVerification('test@example.com');
`:""),dart:`
import 'package:hanzoai/base.dart';

final base = Base('${a[4]}');

...

// example create body
final body = <String, dynamic>${JSON.stringify(a[7](a[0]),null,2)};

final record = await base.collection('${(ot=a[0])==null?void 0:ot.name}').create(body: body);
`+(a[1]?`
// (optional) send an email verification request
await base.collection('${(rt=a[0])==null?void 0:rt.name}').requestVerification('test@example.com');
`:"")}});let J=a[6]&&yt(),V=a[1]&&ht(a),ge=ce(a[5]);const lt=n=>n[15].name;for(let n=0;n<ge.length;n+=1){let d=_t(a,ge,n),v=lt(d);je.set(v,R[n]=vt(v,d))}re=new Ct({props:{content:"?expand=relField1,relField2.subRelField"}}),de=new At({});let Ce=ce(a[3]);const nt=n=>n[10].code;for(let n=0;n<Ce.length;n+=1){let d=bt(a,Ce,n),v=nt(d);Ge.set(v,z[n]=wt(v,d))}let he=ce(a[3]);const it=n=>n[10].code;for(let n=0;n<he.length;n+=1){let d=mt(a,he,n),v=it(d);tt.set(v,j[n]=gt(v,d))}return{c(){e=s("h3"),t=b("Create ("),c=b(l),f=b(")"),u=p(),_=s("div"),m=s("p"),q=b("Create a new "),h=s("strong"),B=b(g),S=b(" record."),$=p(),A=s("p"),A.innerHTML=`Body parameters could be sent as <code>application/json</code> or
        <code>multipart/form-data</code>.`,L=p(),I=s("p"),I.innerHTML=`File upload is supported only via <code>multipart/form-data</code>.
        <br/>
        For more info and examples you could check the detailed
        <a href="undefined" target="_blank" rel="noopener noreferrer">Files upload and handling docs
        </a>.`,D=p(),Te(M.$$.fragment),Q=p(),F=s("h6"),F.textContent="API details",T=p(),y=s("div"),H=s("strong"),H.textContent="POST",ee=p(),x=s("div"),U=s("p"),oe=b("/api/collections/"),K=s("strong"),Y=b(X),ue=b("/records"),te=p(),J&&J.c(),E=p(),Z=s("div"),Z.textContent="Body Parameters",G=p(),O=s("table"),k=s("thead"),k.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr>',C=p(),N=s("tbody"),V&&V.c(),W=p();for(let n=0;n<R.length;n+=1)R[n].c();Me=p(),pe=s("div"),pe.textContent="Query parameters",Le=p(),le=s("table"),Fe=s("thead"),Fe.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',Je=p(),me=s("tbody"),ne=s("tr"),He=s("td"),He.textContent="expand",De=p(),Oe=s("td"),Oe.innerHTML='<span class="label">String</span>',Ee=p(),P=s("td"),Ie=b(`Auto expand relations when returning the created record. Ex.:
                `),Te(re.$$.fragment),Ue=b(`
                Supports up to 6-levels depth nested relations expansion. `),ze=s("br"),Qe=b(`
                The expanded relations will be appended to the record under the
                `),Re=s("code"),Re.textContent="expand",We=b(" property (eg. "),Ae=s("code"),Ae.textContent='"expand": {"relField1": {...}, ...}',xe=b(`).
                `),Ke=s("br"),Xe=b(`
                Only the relations to which the request user has permissions to `),Pe=s("strong"),Pe.textContent="view",Ye=b(" will be expanded."),Ze=p(),Te(de.$$.fragment),Be=p(),be=s("div"),be.textContent="Responses",Ve=p(),ie=s("div"),_e=s("div");for(let n=0;n<z.length;n+=1)z[n].c();et=p(),ye=s("div");for(let n=0;n<j.length;n+=1)j[n].c();w(e,"class","m-b-sm"),w(_,"class","content txt-lg m-b-sm"),w(F,"class","m-b-xs"),w(H,"class","label label-primary"),w(x,"class","content"),w(y,"class","alert alert-success"),w(Z,"class","section-title"),w(O,"class","table-compact table-border m-b-base"),w(pe,"class","section-title"),w(le,"class","table-compact table-border m-b-base"),w(be,"class","section-title"),w(_e,"class","tabs-header compact combined left"),w(ye,"class","tabs-content"),w(ie,"class","tabs")},m(n,d){r(n,e,d),i(e,t),i(e,c),i(e,f),r(n,u,d),r(n,_,d),i(_,m),i(m,q),i(m,h),i(h,B),i(m,S),i(_,$),i(_,A),i(_,L),i(_,I),r(n,D,d),qe(M,n,d),r(n,Q,d),r(n,F,d),r(n,T,d),r(n,y,d),i(y,H),i(y,ee),i(y,x),i(x,U),i(U,oe),i(U,K),i(K,Y),i(U,ue),i(y,te),J&&J.m(y,null),r(n,E,d),r(n,Z,d),r(n,G,d),r(n,O,d),i(O,k),i(O,C),i(O,N),V&&V.m(N,null),i(N,W);for(let v=0;v<R.length;v+=1)R[v]&&R[v].m(N,null);r(n,Me,d),r(n,pe,d),r(n,Le,d),r(n,le,d),i(le,Fe),i(le,Je),i(le,me),i(me,ne),i(ne,He),i(ne,De),i(ne,Oe),i(ne,Ee),i(ne,P),i(P,Ie),qe(re,P,null),i(P,Ue),i(P,ze),i(P,Qe),i(P,Re),i(P,We),i(P,Ae),i(P,xe),i(P,Ke),i(P,Xe),i(P,Pe),i(P,Ye),i(me,Ze),qe(de,me,null),r(n,Be,d),r(n,be,d),r(n,Ve,d),r(n,ie,d),i(ie,_e);for(let v=0;v<z.length;v+=1)z[v]&&z[v].m(_e,null);i(ie,et),i(ie,ye);for(let v=0;v<j.length;v+=1)j[v]&&j[v].m(ye,null);ae=!0},p(n,[d]){var dt,ct,ft,ut;(!ae||d&1)&&l!==(l=n[0].name+"")&&se(c,l),(!ae||d&1)&&g!==(g=n[0].name+"")&&se(B,g);const v={};d&19&&(v.js=`
import Base from 'base';

const base = new Base('${n[4]}');

...

// example create data
const data = ${JSON.stringify(n[7](n[0]),null,4)};

const record = await base.collection('${(dt=n[0])==null?void 0:dt.name}').create(data);
`+(n[1]?`
// (optional) send an email verification request
await base.collection('${(ct=n[0])==null?void 0:ct.name}').requestVerification('test@example.com');
`:"")),d&19&&(v.dart=`
import 'package:hanzoai/base.dart';

final base = Base('${n[4]}');

...

// example create body
final body = <String, dynamic>${JSON.stringify(n[7](n[0]),null,2)};

final record = await base.collection('${(ft=n[0])==null?void 0:ft.name}').create(body: body);
`+(n[1]?`
// (optional) send an email verification request
await base.collection('${(ut=n[0])==null?void 0:ut.name}').requestVerification('test@example.com');
`:"")),M.$set(v),(!ae||d&1)&&X!==(X=n[0].name+"")&&se(Y,X),n[6]?J||(J=yt(),J.c(),J.m(y,null)):J&&(J.d(1),J=null),n[1]?V?V.p(n,d):(V=ht(n),V.c(),V.m(N,W)):V&&(V.d(1),V=null),d&32&&(ge=ce(n[5]),R=Ne(R,d,lt,1,n,ge,je,N,pt,vt,null,_t)),d&12&&(Ce=ce(n[3]),z=Ne(z,d,nt,1,n,Ce,Ge,_e,pt,wt,null,bt)),d&12&&(he=ce(n[3]),Mt(),j=Ne(j,d,it,1,n,he,tt,ye,Lt,gt,null,mt),Ft())},i(n){if(!ae){ve(M.$$.fragment,n),ve(re.$$.fragment,n),ve(de.$$.fragment,n);for(let d=0;d<he.length;d+=1)ve(j[d]);ae=!0}},o(n){ke(M.$$.fragment,n),ke(re.$$.fragment,n),ke(de.$$.fragment,n);for(let d=0;d<j.length;d+=1)ke(j[d]);ae=!1},d(n){n&&(o(e),o(u),o(_),o(D),o(Q),o(F),o(T),o(y),o(E),o(Z),o(G),o(O),o(Me),o(pe),o(Le),o(le),o(Be),o(be),o(Ve),o(ie)),$e(M,n),J&&J.d(),V&&V.d();for(let d=0;d<R.length;d+=1)R[d].d();$e(re),$e(de);for(let d=0;d<z.length;d+=1)z[d].d();for(let d=0;d<j.length;d+=1)j[d].d()}}}const Xt=a=>a.name=="emailVisibility",Yt=a=>a.name=="email";function Zt(a,e,t){let l,c,f,u,_,{collection:m}=e,q=200,h=[];function g(S){let $=we.dummyCollectionSchemaData(S,!0);return l&&($.password="12345678",$.passwordConfirm="12345678",delete $.verified),$}const B=S=>t(2,q=S.code);return a.$$set=S=>{"collection"in S&&t(0,m=S.collection)},a.$$.update=()=>{var S,$,A;a.$$.dirty&1&&t(1,l=m.type==="auth"),a.$$.dirty&1&&t(6,c=(m==null?void 0:m.createRule)===null),a.$$.dirty&2&&t(8,f=l?["password","verified","email","emailVisibility"]:[]),a.$$.dirty&257&&t(5,u=((S=m==null?void 0:m.fields)==null?void 0:S.filter(L=>!L.hidden&&L.type!="autodate"&&!f.includes(L.name)))||[]),a.$$.dirty&1&&t(3,h=[{code:200,body:JSON.stringify(we.dummyCollectionRecord(m),null,2)},{code:400,body:`
                {
                  "status": 400,
                  "message": "Failed to create record.",
                  "data": {
                    "${(A=($=m==null?void 0:m.fields)==null?void 0:$[0])==null?void 0:A.name}": {
                      "code": "validation_required",
                      "message": "Missing required value."
                    }
                  }
                }
            `},{code:403,body:`
                {
                  "status": 403,
                  "message": "You are not allowed to perform this request.",
                  "data": {}
                }
            `}])},t(4,_=we.getApiExampleUrl(Ht.baseURL)),[m,l,q,h,_,u,c,g,f,B]}class tl extends $t{constructor(e){super(),qt(this,e,Zt,Kt,Tt,{collection:0})}}export{tl as default};
