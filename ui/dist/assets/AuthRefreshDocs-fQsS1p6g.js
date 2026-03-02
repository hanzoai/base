import{S as Ue,i as xe,s as Ie,V as Ke,W as Ne,X as I,j as c,d as K,t as E,a as P,I as ce,Z as Oe,_ as Qe,C as We,$ as Xe,D as Ze,n as u,o as a,m as Q,u as o,A as g,v as h,c as W,w as b,J as Ve,b as Ge,l as X,p as Ye}from"./index-D7eFJ7oc.js";import{F as et}from"./FieldsQueryParam-Car0Lphb.js";function Ee(r,s,l){const n=r.slice();return n[5]=s[l],n}function Pe(r,s,l){const n=r.slice();return n[5]=s[l],n}function je(r,s){let l,n=s[5].code+"",m,_,i,f;function k(){return s[4](s[5])}return{key:r,first:null,c(){l=o("button"),m=g(n),_=h(),b(l,"class","tab-item"),X(l,"active",s[1]===s[5].code),this.first=l},m(v,w){u(v,l,w),a(l,m),a(l,_),i||(f=Ye(l,"click",k),i=!0)},p(v,w){s=v,w&4&&n!==(n=s[5].code+"")&&ce(m,n),w&6&&X(l,"active",s[1]===s[5].code)},d(v){v&&c(l),i=!1,f()}}}function Je(r,s){let l,n,m,_;return n=new Ne({props:{content:s[5].body}}),{key:r,first:null,c(){l=o("div"),W(n.$$.fragment),m=h(),b(l,"class","tab-item"),X(l,"active",s[1]===s[5].code),this.first=l},m(i,f){u(i,l,f),Q(n,l,null),a(l,m),_=!0},p(i,f){s=i;const k={};f&4&&(k.content=s[5].body),n.$set(k),(!_||f&6)&&X(l,"active",s[1]===s[5].code)},i(i){_||(P(n.$$.fragment,i),_=!0)},o(i){E(n.$$.fragment,i),_=!1},d(i){i&&c(l),K(n)}}}function tt(r){var Fe,ze;let s,l,n=r[0].name+"",m,_,i,f,k,v,w,M,Z,S,j,ue,J,q,he,G,N=r[0].name+"",Y,fe,pe,U,ee,F,te,T,ae,be,z,C,se,me,le,_e,p,ge,A,ke,ve,$e,oe,ye,ne,Se,we,Te,re,Ce,Re,B,ie,H,de,R,L,y=[],Ae=new Map,Be,O,$=[],De=new Map,D;v=new Ke({props:{js:`
        import Base from 'base';

        const base = new Base('${r[3]}');

        ...

        const authData = await base.collection('${(Fe=r[0])==null?void 0:Fe.name}').authRefresh();

        // after the above you can also access the refreshed auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${r[3]}');

        ...

        final authData = await base.collection('${(ze=r[0])==null?void 0:ze.name}').authRefresh();

        // after the above you can also access the refreshed auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);
    `}}),A=new Ne({props:{content:"?expand=relField1,relField2.subRelField"}}),B=new et({props:{prefix:"record."}});let x=I(r[2]);const Me=e=>e[5].code;for(let e=0;e<x.length;e+=1){let t=Pe(r,x,e),d=Me(t);Ae.set(d,y[e]=je(d,t))}let V=I(r[2]);const qe=e=>e[5].code;for(let e=0;e<V.length;e+=1){let t=Ee(r,V,e),d=qe(t);De.set(d,$[e]=Je(d,t))}return{c(){s=o("h3"),l=g("Auth refresh ("),m=g(n),_=g(")"),i=h(),f=o("div"),f.innerHTML=`<p>Returns a new auth response (token and record data) for an
        <strong>already authenticated record</strong>.</p> <p>This method is usually called by users on page/screen reload to ensure that the previously stored data
        in <code>base.authStore</code> is still valid and up-to-date.</p>`,k=h(),W(v.$$.fragment),w=h(),M=o("h6"),M.textContent="API details",Z=h(),S=o("div"),j=o("strong"),j.textContent="POST",ue=h(),J=o("div"),q=o("p"),he=g("/api/collections/"),G=o("strong"),Y=g(N),fe=g("/auth-refresh"),pe=h(),U=o("p"),U.innerHTML="Requires <code>Authorization:TOKEN</code> header",ee=h(),F=o("div"),F.textContent="Query parameters",te=h(),T=o("table"),ae=o("thead"),ae.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',be=h(),z=o("tbody"),C=o("tr"),se=o("td"),se.textContent="expand",me=h(),le=o("td"),le.innerHTML='<span class="label">String</span>',_e=h(),p=o("td"),ge=g(`Auto expand record relations. Ex.:
                `),W(A.$$.fragment),ke=g(`
                Supports up to 6-levels depth nested relations expansion. `),ve=o("br"),$e=g(`
                The expanded relations will be appended to the record under the
                `),oe=o("code"),oe.textContent="expand",ye=g(" property (eg. "),ne=o("code"),ne.textContent='"expand": {"relField1": {...}, ...}',Se=g(`).
                `),we=o("br"),Te=g(`
                Only the relations to which the request user has permissions to `),re=o("strong"),re.textContent="view",Ce=g(" will be expanded."),Re=h(),W(B.$$.fragment),ie=h(),H=o("div"),H.textContent="Responses",de=h(),R=o("div"),L=o("div");for(let e=0;e<y.length;e+=1)y[e].c();Be=h(),O=o("div");for(let e=0;e<$.length;e+=1)$[e].c();b(s,"class","m-b-sm"),b(f,"class","content txt-lg m-b-sm"),b(M,"class","m-b-xs"),b(j,"class","label label-primary"),b(J,"class","content"),b(U,"class","txt-hint txt-sm txt-right"),b(S,"class","alert alert-success"),b(F,"class","section-title"),b(T,"class","table-compact table-border m-b-base"),b(H,"class","section-title"),b(L,"class","tabs-header compact combined left"),b(O,"class","tabs-content"),b(R,"class","tabs")},m(e,t){u(e,s,t),a(s,l),a(s,m),a(s,_),u(e,i,t),u(e,f,t),u(e,k,t),Q(v,e,t),u(e,w,t),u(e,M,t),u(e,Z,t),u(e,S,t),a(S,j),a(S,ue),a(S,J),a(J,q),a(q,he),a(q,G),a(G,Y),a(q,fe),a(S,pe),a(S,U),u(e,ee,t),u(e,F,t),u(e,te,t),u(e,T,t),a(T,ae),a(T,be),a(T,z),a(z,C),a(C,se),a(C,me),a(C,le),a(C,_e),a(C,p),a(p,ge),Q(A,p,null),a(p,ke),a(p,ve),a(p,$e),a(p,oe),a(p,ye),a(p,ne),a(p,Se),a(p,we),a(p,Te),a(p,re),a(p,Ce),a(z,Re),Q(B,z,null),u(e,ie,t),u(e,H,t),u(e,de,t),u(e,R,t),a(R,L);for(let d=0;d<y.length;d+=1)y[d]&&y[d].m(L,null);a(R,Be),a(R,O);for(let d=0;d<$.length;d+=1)$[d]&&$[d].m(O,null);D=!0},p(e,[t]){var He,Le;(!D||t&1)&&n!==(n=e[0].name+"")&&ce(m,n);const d={};t&9&&(d.js=`
        import Base from 'base';

        const base = new Base('${e[3]}');

        ...

        const authData = await base.collection('${(He=e[0])==null?void 0:He.name}').authRefresh();

        // after the above you can also access the refreshed auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);
    `),t&9&&(d.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${e[3]}');

        ...

        final authData = await base.collection('${(Le=e[0])==null?void 0:Le.name}').authRefresh();

        // after the above you can also access the refreshed auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);
    `),v.$set(d),(!D||t&1)&&N!==(N=e[0].name+"")&&ce(Y,N),t&6&&(x=I(e[2]),y=Oe(y,t,Me,1,e,x,Ae,L,Qe,je,null,Pe)),t&6&&(V=I(e[2]),We(),$=Oe($,t,qe,1,e,V,De,O,Xe,Je,null,Ee),Ze())},i(e){if(!D){P(v.$$.fragment,e),P(A.$$.fragment,e),P(B.$$.fragment,e);for(let t=0;t<V.length;t+=1)P($[t]);D=!0}},o(e){E(v.$$.fragment,e),E(A.$$.fragment,e),E(B.$$.fragment,e);for(let t=0;t<$.length;t+=1)E($[t]);D=!1},d(e){e&&(c(s),c(i),c(f),c(k),c(w),c(M),c(Z),c(S),c(ee),c(F),c(te),c(T),c(ie),c(H),c(de),c(R)),K(v,e),K(A),K(B);for(let t=0;t<y.length;t+=1)y[t].d();for(let t=0;t<$.length;t+=1)$[t].d()}}}function at(r,s,l){let n,{collection:m}=s,_=200,i=[];const f=k=>l(1,_=k.code);return r.$$set=k=>{"collection"in k&&l(0,m=k.collection)},r.$$.update=()=>{r.$$.dirty&1&&l(2,i=[{code:200,body:JSON.stringify({token:"JWT_TOKEN",record:Ve.dummyCollectionRecord(m)},null,2)},{code:401,body:`
                {
                  "status": 401,
                  "message": "The request requires valid record authorization token to be set.",
                  "data": {}
                }
            `},{code:403,body:`
                {
                  "status": 403,
                  "message": "The authorized record model is not allowed to perform this action.",
                  "data": {}
                }
            `},{code:404,body:`
                {
                  "status": 404,
                  "message": "Missing auth record context.",
                  "data": {}
                }
            `}])},l(3,n=Ve.getApiExampleUrl(Ge.baseURL)),[m,_,i,n,f]}class ot extends Ue{constructor(s){super(),xe(this,s,at,tt,Ie,{collection:0})}}export{ot as default};
