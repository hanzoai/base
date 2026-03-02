import{S as $t,i as Mt,s as St,V as Ot,X as ie,W as Tt,j as d,d as we,t as _e,a as he,I as ee,Z as Je,_ as bt,C as qt,$ as Rt,D as Ht,n as o,o as a,m as ge,u as i,A as _,v as f,c as Ce,w as k,J as ye,b as Lt,l as Te,p as Dt,H as te}from"./index-D7eFJ7oc.js";import{F as Ft}from"./FieldsQueryParam-Car0Lphb.js";function mt(r,e,t){const n=r.slice();return n[10]=e[t],n}function _t(r,e,t){const n=r.slice();return n[10]=e[t],n}function ht(r,e,t){const n=r.slice();return n[15]=e[t],n}function yt(r){let e;return{c(){e=i("p"),e.innerHTML=`<em>Note that in case of a password change all previously issued tokens for the current record
                will be automatically invalidated and if you want your user to remain signed in you need to
                reauthenticate manually after the update call.</em>`},m(t,n){o(t,e,n)},d(t){t&&d(e)}}}function kt(r){let e;return{c(){e=i("p"),e.innerHTML="Requires superuser <code>Authorization:TOKEN</code> header",k(e,"class","txt-hint txt-sm txt-right")},m(t,n){o(t,e,n)},d(t){t&&d(e)}}}function vt(r){let e,t,n,b,p,c,u,m,S,T,H,L,$,M,q,D,J,I,O,R,F,v,w,g;function x(h,C){var le,Q,ne;return C&1&&(m=null),m==null&&(m=!!((ne=(Q=(le=h[0])==null?void 0:le.fields)==null?void 0:Q.find(Wt))!=null&&ne.required)),m?Bt:Pt}let z=x(r,-1),B=z(r);return{c(){e=i("tr"),e.innerHTML='<td colspan="3" class="txt-hint txt-bold">Auth specific fields</td>',t=f(),n=i("tr"),n.innerHTML=`<td><div class="inline-flex"><span class="label label-warning">Optional</span> <span>email</span></div></td> <td><span class="label">String</span></td> <td>The auth record email address.
                    <br/>
                    This field can be updated only by superusers or auth records with &quot;Manage&quot; access.
                    <br/>
                    Regular accounts can update their email by calling &quot;Request email change&quot;.</td>`,b=f(),p=i("tr"),c=i("td"),u=i("div"),B.c(),S=f(),T=i("span"),T.textContent="emailVisibility",H=f(),L=i("td"),L.innerHTML='<span class="label">Boolean</span>',$=f(),M=i("td"),M.textContent="Whether to show/hide the auth record email when fetching the record data.",q=f(),D=i("tr"),D.innerHTML=`<td><div class="inline-flex"><span class="label label-warning">Optional</span> <span>oldPassword</span></div></td> <td><span class="label">String</span></td> <td>Old auth record password.
                    <br/>
                    This field is required only when changing the record password. Superusers and auth records
                    with &quot;Manage&quot; access can skip this field.</td>`,J=f(),I=i("tr"),I.innerHTML='<td><div class="inline-flex"><span class="label label-warning">Optional</span> <span>password</span></div></td> <td><span class="label">String</span></td> <td>New auth record password.</td>',O=f(),R=i("tr"),R.innerHTML='<td><div class="inline-flex"><span class="label label-warning">Optional</span> <span>passwordConfirm</span></div></td> <td><span class="label">String</span></td> <td>New auth record password confirmation.</td>',F=f(),v=i("tr"),v.innerHTML=`<td><div class="inline-flex"><span class="label label-warning">Optional</span> <span>verified</span></div></td> <td><span class="label">Boolean</span></td> <td>Indicates whether the auth record is verified or not.
                    <br/>
                    This field can be set only by superusers or auth records with &quot;Manage&quot; access.</td>`,w=f(),g=i("tr"),g.innerHTML='<td colspan="3" class="txt-hint txt-bold">Other fields</td>',k(u,"class","inline-flex")},m(h,C){o(h,e,C),o(h,t,C),o(h,n,C),o(h,b,C),o(h,p,C),a(p,c),a(c,u),B.m(u,null),a(u,S),a(u,T),a(p,H),a(p,L),a(p,$),a(p,M),o(h,q,C),o(h,D,C),o(h,J,C),o(h,I,C),o(h,O,C),o(h,R,C),o(h,F,C),o(h,v,C),o(h,w,C),o(h,g,C)},p(h,C){z!==(z=x(h,C))&&(B.d(1),B=z(h),B&&(B.c(),B.m(u,S)))},d(h){h&&(d(e),d(t),d(n),d(b),d(p),d(q),d(D),d(J),d(I),d(O),d(R),d(F),d(v),d(w),d(g)),B.d()}}}function Pt(r){let e;return{c(){e=i("span"),e.textContent="Optional",k(e,"class","label label-warning")},m(t,n){o(t,e,n)},d(t){t&&d(e)}}}function Bt(r){let e;return{c(){e=i("span"),e.textContent="Required",k(e,"class","label label-success")},m(t,n){o(t,e,n)},d(t){t&&d(e)}}}function Nt(r){let e;return{c(){e=i("span"),e.textContent="Optional",k(e,"class","label label-warning")},m(t,n){o(t,e,n)},d(t){t&&d(e)}}}function At(r){let e;return{c(){e=i("span"),e.textContent="Required",k(e,"class","label label-success")},m(t,n){o(t,e,n)},d(t){t&&d(e)}}}function jt(r){let e,t=r[15].maxSelect==1?"id":"ids",n,b;return{c(){e=_("Relation record "),n=_(t),b=_(".")},m(p,c){o(p,e,c),o(p,n,c),o(p,b,c)},p(p,c){c&32&&t!==(t=p[15].maxSelect==1?"id":"ids")&&ee(n,t)},d(p){p&&(d(e),d(n),d(b))}}}function Et(r){let e,t,n,b,p;return{c(){e=_("File object."),t=i("br"),n=_(`
                        Set to `),b=i("code"),b.textContent="null",p=_(" to delete already uploaded file(s).")},m(c,u){o(c,e,u),o(c,t,u),o(c,n,u),o(c,b,u),o(c,p,u)},p:te,d(c){c&&(d(e),d(t),d(n),d(b),d(p))}}}function It(r){let e,t;return{c(){e=i("code"),e.textContent='{"lon":x,"lat":y}',t=_(" object.")},m(n,b){o(n,e,b),o(n,t,b)},p:te,d(n){n&&(d(e),d(t))}}}function Jt(r){let e;return{c(){e=_("URL address.")},m(t,n){o(t,e,n)},p:te,d(t){t&&d(e)}}}function Ut(r){let e;return{c(){e=_("Email address.")},m(t,n){o(t,e,n)},p:te,d(t){t&&d(e)}}}function Vt(r){let e;return{c(){e=_("JSON array or object.")},m(t,n){o(t,e,n)},p:te,d(t){t&&d(e)}}}function xt(r){let e;return{c(){e=_("Number value.")},m(t,n){o(t,e,n)},p:te,d(t){t&&d(e)}}}function zt(r){let e;return{c(){e=_("Plain text value.")},m(t,n){o(t,e,n)},p:te,d(t){t&&d(e)}}}function wt(r,e){let t,n,b,p,c,u=e[15].name+"",m,S,T,H,L=ye.getFieldValueType(e[15])+"",$,M,q,D;function J(w,g){return w[15].required?At:Nt}let I=J(e),O=I(e);function R(w,g){if(w[15].type==="text")return zt;if(w[15].type==="number")return xt;if(w[15].type==="json")return Vt;if(w[15].type==="email")return Ut;if(w[15].type==="url")return Jt;if(w[15].type==="geoPoint")return It;if(w[15].type==="file")return Et;if(w[15].type==="relation")return jt}let F=R(e),v=F&&F(e);return{key:r,first:null,c(){t=i("tr"),n=i("td"),b=i("div"),O.c(),p=f(),c=i("span"),m=_(u),S=f(),T=i("td"),H=i("span"),$=_(L),M=f(),q=i("td"),v&&v.c(),D=f(),k(b,"class","inline-flex"),k(H,"class","label"),this.first=t},m(w,g){o(w,t,g),a(t,n),a(n,b),O.m(b,null),a(b,p),a(b,c),a(c,m),a(t,S),a(t,T),a(T,H),a(H,$),a(t,M),a(t,q),v&&v.m(q,null),a(t,D)},p(w,g){e=w,I!==(I=J(e))&&(O.d(1),O=I(e),O&&(O.c(),O.m(b,p))),g&32&&u!==(u=e[15].name+"")&&ee(m,u),g&32&&L!==(L=ye.getFieldValueType(e[15])+"")&&ee($,L),F===(F=R(e))&&v?v.p(e,g):(v&&v.d(1),v=F&&F(e),v&&(v.c(),v.m(q,null)))},d(w){w&&d(t),O.d(),v&&v.d()}}}function gt(r,e){let t,n=e[10].code+"",b,p,c,u;function m(){return e[9](e[10])}return{key:r,first:null,c(){t=i("button"),b=_(n),p=f(),k(t,"class","tab-item"),Te(t,"active",e[2]===e[10].code),this.first=t},m(S,T){o(S,t,T),a(t,b),a(t,p),c||(u=Dt(t,"click",m),c=!0)},p(S,T){e=S,T&8&&n!==(n=e[10].code+"")&&ee(b,n),T&12&&Te(t,"active",e[2]===e[10].code)},d(S){S&&d(t),c=!1,u()}}}function Ct(r,e){let t,n,b,p;return n=new Tt({props:{content:e[10].body}}),{key:r,first:null,c(){t=i("div"),Ce(n.$$.fragment),b=f(),k(t,"class","tab-item"),Te(t,"active",e[2]===e[10].code),this.first=t},m(c,u){o(c,t,u),ge(n,t,null),a(t,b),p=!0},p(c,u){e=c;const m={};u&8&&(m.content=e[10].body),n.$set(m),(!p||u&12)&&Te(t,"active",e[2]===e[10].code)},i(c){p||(he(n.$$.fragment,c),p=!0)},o(c){_e(n.$$.fragment,c),p=!1},d(c){c&&d(t),we(n)}}}function Qt(r){var ct,ut;let e,t,n=r[0].name+"",b,p,c,u,m,S,T,H=r[0].name+"",L,$,M,q,D,J,I,O,R,F,v,w,g,x,z,B,h,C,le,Q=r[0].name+"",ne,Ue,$e,Ve,Me,de,Se,oe,Oe,re,qe,W,Re,xe,K,He,U=[],ze=new Map,Le,ce,De,X,Fe,Qe,ue,Y,Pe,We,Be,Ke,N,Xe,ae,Ye,Ze,Ge,Ne,et,Ae,tt,je,lt,nt,se,Ee,pe,Ie,Z,fe,V=[],at=new Map,st,be,A=[],it=new Map,G,j=r[1]&&yt();R=new Ot({props:{js:`
import Base from 'base';

const base = new Base('${r[4]}');

...

// example update data
const data = ${JSON.stringify(r[7](r[0]),null,4)};

const record = await base.collection('${(ct=r[0])==null?void 0:ct.name}').update('RECORD_ID', data);
    `,dart:`
import 'package:hanzoai/base.dart';

final base = Base('${r[4]}');

...

// example update body
final body = <String, dynamic>${JSON.stringify(r[7](r[0]),null,2)};

final record = await base.collection('${(ut=r[0])==null?void 0:ut.name}').update('RECORD_ID', body: body);
    `}});let E=r[6]&&kt(),P=r[1]&&vt(r),ke=ie(r[5]);const dt=l=>l[15].name;for(let l=0;l<ke.length;l+=1){let s=ht(r,ke,l),y=dt(s);ze.set(y,U[l]=wt(y,s))}ae=new Tt({props:{content:"?expand=relField1,relField2.subRelField21"}}),se=new Ft({});let ve=ie(r[3]);const ot=l=>l[10].code;for(let l=0;l<ve.length;l+=1){let s=_t(r,ve,l),y=ot(s);at.set(y,V[l]=gt(y,s))}let me=ie(r[3]);const rt=l=>l[10].code;for(let l=0;l<me.length;l+=1){let s=mt(r,me,l),y=rt(s);it.set(y,A[l]=Ct(y,s))}return{c(){e=i("h3"),t=_("Update ("),b=_(n),p=_(")"),c=f(),u=i("div"),m=i("p"),S=_("Update a single "),T=i("strong"),L=_(H),$=_(" record."),M=f(),q=i("p"),q.innerHTML=`Body parameters could be sent as <code>application/json</code> or
        <code>multipart/form-data</code>.`,D=f(),J=i("p"),J.innerHTML=`File upload is supported only via <code>multipart/form-data</code>.
        <br/>
        For more info and examples you could check the detailed
        <a href="undefined" target="_blank" rel="noopener noreferrer">Files upload and handling docs
        </a>.`,I=f(),j&&j.c(),O=f(),Ce(R.$$.fragment),F=f(),v=i("h6"),v.textContent="API details",w=f(),g=i("div"),x=i("strong"),x.textContent="PATCH",z=f(),B=i("div"),h=i("p"),C=_("/api/collections/"),le=i("strong"),ne=_(Q),Ue=_("/records/"),$e=i("strong"),$e.textContent=":id",Ve=f(),E&&E.c(),Me=f(),de=i("div"),de.textContent="Path parameters",Se=f(),oe=i("table"),oe.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr></thead> <tbody><tr><td>id</td> <td><span class="label">String</span></td> <td>ID of the record to update.</td></tr></tbody>',Oe=f(),re=i("div"),re.textContent="Body Parameters",qe=f(),W=i("table"),Re=i("thead"),Re.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr>',xe=f(),K=i("tbody"),P&&P.c(),He=f();for(let l=0;l<U.length;l+=1)U[l].c();Le=f(),ce=i("div"),ce.textContent="Query parameters",De=f(),X=i("table"),Fe=i("thead"),Fe.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',Qe=f(),ue=i("tbody"),Y=i("tr"),Pe=i("td"),Pe.textContent="expand",We=f(),Be=i("td"),Be.innerHTML='<span class="label">String</span>',Ke=f(),N=i("td"),Xe=_(`Auto expand relations when returning the updated record. Ex.:
                `),Ce(ae.$$.fragment),Ye=_(`
                Supports up to 6-levels depth nested relations expansion. `),Ze=i("br"),Ge=_(`
                The expanded relations will be appended to the record under the
                `),Ne=i("code"),Ne.textContent="expand",et=_(" property (eg. "),Ae=i("code"),Ae.textContent='"expand": {"relField1": {...}, ...}',tt=_(`). Only
                the relations that the user has permissions to `),je=i("strong"),je.textContent="view",lt=_(" will be expanded."),nt=f(),Ce(se.$$.fragment),Ee=f(),pe=i("div"),pe.textContent="Responses",Ie=f(),Z=i("div"),fe=i("div");for(let l=0;l<V.length;l+=1)V[l].c();st=f(),be=i("div");for(let l=0;l<A.length;l+=1)A[l].c();k(e,"class","m-b-sm"),k(u,"class","content txt-lg m-b-sm"),k(v,"class","m-b-xs"),k(x,"class","label label-primary"),k(B,"class","content"),k(g,"class","alert alert-warning"),k(de,"class","section-title"),k(oe,"class","table-compact table-border m-b-base"),k(re,"class","section-title"),k(W,"class","table-compact table-border m-b-base"),k(ce,"class","section-title"),k(X,"class","table-compact table-border m-b-lg"),k(pe,"class","section-title"),k(fe,"class","tabs-header compact combined left"),k(be,"class","tabs-content"),k(Z,"class","tabs")},m(l,s){o(l,e,s),a(e,t),a(e,b),a(e,p),o(l,c,s),o(l,u,s),a(u,m),a(m,S),a(m,T),a(T,L),a(m,$),a(u,M),a(u,q),a(u,D),a(u,J),a(u,I),j&&j.m(u,null),o(l,O,s),ge(R,l,s),o(l,F,s),o(l,v,s),o(l,w,s),o(l,g,s),a(g,x),a(g,z),a(g,B),a(B,h),a(h,C),a(h,le),a(le,ne),a(h,Ue),a(h,$e),a(g,Ve),E&&E.m(g,null),o(l,Me,s),o(l,de,s),o(l,Se,s),o(l,oe,s),o(l,Oe,s),o(l,re,s),o(l,qe,s),o(l,W,s),a(W,Re),a(W,xe),a(W,K),P&&P.m(K,null),a(K,He);for(let y=0;y<U.length;y+=1)U[y]&&U[y].m(K,null);o(l,Le,s),o(l,ce,s),o(l,De,s),o(l,X,s),a(X,Fe),a(X,Qe),a(X,ue),a(ue,Y),a(Y,Pe),a(Y,We),a(Y,Be),a(Y,Ke),a(Y,N),a(N,Xe),ge(ae,N,null),a(N,Ye),a(N,Ze),a(N,Ge),a(N,Ne),a(N,et),a(N,Ae),a(N,tt),a(N,je),a(N,lt),a(ue,nt),ge(se,ue,null),o(l,Ee,s),o(l,pe,s),o(l,Ie,s),o(l,Z,s),a(Z,fe);for(let y=0;y<V.length;y+=1)V[y]&&V[y].m(fe,null);a(Z,st),a(Z,be);for(let y=0;y<A.length;y+=1)A[y]&&A[y].m(be,null);G=!0},p(l,[s]){var pt,ft;(!G||s&1)&&n!==(n=l[0].name+"")&&ee(b,n),(!G||s&1)&&H!==(H=l[0].name+"")&&ee(L,H),l[1]?j||(j=yt(),j.c(),j.m(u,null)):j&&(j.d(1),j=null);const y={};s&17&&(y.js=`
import Base from 'base';

const base = new Base('${l[4]}');

...

// example update data
const data = ${JSON.stringify(l[7](l[0]),null,4)};

const record = await base.collection('${(pt=l[0])==null?void 0:pt.name}').update('RECORD_ID', data);
    `),s&17&&(y.dart=`
import 'package:hanzoai/base.dart';

final base = Base('${l[4]}');

...

// example update body
final body = <String, dynamic>${JSON.stringify(l[7](l[0]),null,2)};

final record = await base.collection('${(ft=l[0])==null?void 0:ft.name}').update('RECORD_ID', body: body);
    `),R.$set(y),(!G||s&1)&&Q!==(Q=l[0].name+"")&&ee(ne,Q),l[6]?E||(E=kt(),E.c(),E.m(g,null)):E&&(E.d(1),E=null),l[1]?P?P.p(l,s):(P=vt(l),P.c(),P.m(K,He)):P&&(P.d(1),P=null),s&32&&(ke=ie(l[5]),U=Je(U,s,dt,1,l,ke,ze,K,bt,wt,null,ht)),s&12&&(ve=ie(l[3]),V=Je(V,s,ot,1,l,ve,at,fe,bt,gt,null,_t)),s&12&&(me=ie(l[3]),qt(),A=Je(A,s,rt,1,l,me,it,be,Rt,Ct,null,mt),Ht())},i(l){if(!G){he(R.$$.fragment,l),he(ae.$$.fragment,l),he(se.$$.fragment,l);for(let s=0;s<me.length;s+=1)he(A[s]);G=!0}},o(l){_e(R.$$.fragment,l),_e(ae.$$.fragment,l),_e(se.$$.fragment,l);for(let s=0;s<A.length;s+=1)_e(A[s]);G=!1},d(l){l&&(d(e),d(c),d(u),d(O),d(F),d(v),d(w),d(g),d(Me),d(de),d(Se),d(oe),d(Oe),d(re),d(qe),d(W),d(Le),d(ce),d(De),d(X),d(Ee),d(pe),d(Ie),d(Z)),j&&j.d(),we(R,l),E&&E.d(),P&&P.d();for(let s=0;s<U.length;s+=1)U[s].d();we(ae),we(se);for(let s=0;s<V.length;s+=1)V[s].d();for(let s=0;s<A.length;s+=1)A[s].d()}}}const Wt=r=>r.name=="emailVisibility";function Kt(r,e,t){let n,b,p,c,u,{collection:m}=e,S=200,T=[];function H($){let M=ye.dummyCollectionSchemaData($,!0);return n&&(M.oldPassword="12345678",M.password="87654321",M.passwordConfirm="87654321",delete M.verified,delete M.email),M}const L=$=>t(2,S=$.code);return r.$$set=$=>{"collection"in $&&t(0,m=$.collection)},r.$$.update=()=>{var $,M,q;r.$$.dirty&1&&t(1,n=(m==null?void 0:m.type)==="auth"),r.$$.dirty&1&&t(6,b=(m==null?void 0:m.updateRule)===null),r.$$.dirty&2&&t(8,p=n?["id","password","verified","email","emailVisibility"]:["id"]),r.$$.dirty&257&&t(5,c=(($=m==null?void 0:m.fields)==null?void 0:$.filter(D=>!D.hidden&&D.type!="autodate"&&!p.includes(D.name)))||[]),r.$$.dirty&1&&t(3,T=[{code:200,body:JSON.stringify(ye.dummyCollectionRecord(m),null,2)},{code:400,body:`
                {
                  "status": 400,
                  "message": "Failed to update record.",
                  "data": {
                    "${(q=(M=m==null?void 0:m.fields)==null?void 0:M[0])==null?void 0:q.name}": {
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
            `},{code:404,body:`
                {
                  "status": 404,
                  "message": "The requested resource wasn't found.",
                  "data": {}
                }
            `}])},t(4,u=ye.getApiExampleUrl(Lt.baseURL)),[m,n,S,T,u,c,b,H,p,L]}class Zt extends $t{constructor(e){super(),Mt(this,e,Kt,Qt,St,{collection:0})}}export{Zt as default};
