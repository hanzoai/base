import{S as Be,i as Te,s as Le,V as Se,X as I,j as u,d as ne,t as J,a as Q,I as G,Z as we,_ as je,C as De,$ as Pe,D as Re,n as d,o as n,m as oe,u as c,A as y,v as k,c as ie,w as _,b as ae,J as Ue,l as N,p as qe,W as ze}from"./index-CzlWNNWT.js";import{F as Ee}from"./FieldsQueryParam-BjMsFtDw.js";function ye(o,s,l){const a=o.slice();return a[8]=s[l],a}function Me(o,s,l){const a=o.slice();return a[8]=s[l],a}function Ae(o,s){let l,a=s[8].code+"",h,b,i,f;function m(){return s[6](s[8])}return{key:o,first:null,c(){l=c("button"),h=y(a),b=k(),_(l,"class","tab-item"),N(l,"active",s[1]===s[8].code),this.first=l},m(v,$){d(v,l,$),n(l,h),n(l,b),i||(f=qe(l,"click",m),i=!0)},p(v,$){s=v,$&4&&a!==(a=s[8].code+"")&&G(h,a),$&6&&N(l,"active",s[1]===s[8].code)},d(v){v&&u(l),i=!1,f()}}}function Ce(o,s){let l,a,h,b;return a=new ze({props:{content:s[8].body}}),{key:o,first:null,c(){l=c("div"),ie(a.$$.fragment),h=k(),_(l,"class","tab-item"),N(l,"active",s[1]===s[8].code),this.first=l},m(i,f){d(i,l,f),oe(a,l,null),n(l,h),b=!0},p(i,f){s=i;const m={};f&4&&(m.content=s[8].body),a.$set(m),(!b||f&6)&&N(l,"active",s[1]===s[8].code)},i(i){b||(Q(a.$$.fragment,i),b=!0)},o(i){J(a.$$.fragment,i),b=!1},d(i){i&&u(l),ne(a)}}}function Fe(o){var ke,ge;let s,l,a=o[0].name+"",h,b,i,f,m,v,$,g=o[0].name+"",O,ce,V,M,W,S,X,A,z,re,E,j,ue,Z,F=o[0].name+"",K,de,Y,D,x,C,ee,fe,te,L,le,P,se,B,R,w=[],me=new Map,he,U,p=[],be=new Map,T;M=new Se({props:{js:`
        import Base from 'base';

        const base = new Base('${o[3]}');

        ...

        const result = await base.collection('${(ke=o[0])==null?void 0:ke.name}').listAuthMethods();
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${o[3]}');

        ...

        final result = await base.collection('${(ge=o[0])==null?void 0:ge.name}').listAuthMethods();
    `}}),L=new Ee({});let H=I(o[2]);const _e=e=>e[8].code;for(let e=0;e<H.length;e+=1){let t=Me(o,H,e),r=_e(t);me.set(r,w[e]=Ae(r,t))}let q=I(o[2]);const pe=e=>e[8].code;for(let e=0;e<q.length;e+=1){let t=ye(o,q,e),r=pe(t);be.set(r,p[e]=Ce(r,t))}return{c(){s=c("h3"),l=y("List auth methods ("),h=y(a),b=y(")"),i=k(),f=c("div"),m=c("p"),v=y("Returns a public list with all allowed "),$=c("strong"),O=y(g),ce=y(" authentication methods."),V=k(),ie(M.$$.fragment),W=k(),S=c("h6"),S.textContent="API details",X=k(),A=c("div"),z=c("strong"),z.textContent="GET",re=k(),E=c("div"),j=c("p"),ue=y("/api/collections/"),Z=c("strong"),K=y(F),de=y("/auth-methods"),Y=k(),D=c("div"),D.textContent="Query parameters",x=k(),C=c("table"),ee=c("thead"),ee.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr>',fe=k(),te=c("tbody"),ie(L.$$.fragment),le=k(),P=c("div"),P.textContent="Responses",se=k(),B=c("div"),R=c("div");for(let e=0;e<w.length;e+=1)w[e].c();he=k(),U=c("div");for(let e=0;e<p.length;e+=1)p[e].c();_(s,"class","m-b-sm"),_(f,"class","content txt-lg m-b-sm"),_(S,"class","m-b-xs"),_(z,"class","label label-primary"),_(E,"class","content"),_(A,"class","alert alert-info"),_(D,"class","section-title"),_(C,"class","table-compact table-border m-b-base"),_(P,"class","section-title"),_(R,"class","tabs-header compact combined left"),_(U,"class","tabs-content"),_(B,"class","tabs")},m(e,t){d(e,s,t),n(s,l),n(s,h),n(s,b),d(e,i,t),d(e,f,t),n(f,m),n(m,v),n(m,$),n($,O),n(m,ce),d(e,V,t),oe(M,e,t),d(e,W,t),d(e,S,t),d(e,X,t),d(e,A,t),n(A,z),n(A,re),n(A,E),n(E,j),n(j,ue),n(j,Z),n(Z,K),n(j,de),d(e,Y,t),d(e,D,t),d(e,x,t),d(e,C,t),n(C,ee),n(C,fe),n(C,te),oe(L,te,null),d(e,le,t),d(e,P,t),d(e,se,t),d(e,B,t),n(B,R);for(let r=0;r<w.length;r+=1)w[r]&&w[r].m(R,null);n(B,he),n(B,U);for(let r=0;r<p.length;r+=1)p[r]&&p[r].m(U,null);T=!0},p(e,[t]){var ve,$e;(!T||t&1)&&a!==(a=e[0].name+"")&&G(h,a),(!T||t&1)&&g!==(g=e[0].name+"")&&G(O,g);const r={};t&9&&(r.js=`
        import Base from 'base';

        const base = new Base('${e[3]}');

        ...

        const result = await base.collection('${(ve=e[0])==null?void 0:ve.name}').listAuthMethods();
    `),t&9&&(r.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${e[3]}');

        ...

        final result = await base.collection('${($e=e[0])==null?void 0:$e.name}').listAuthMethods();
    `),M.$set(r),(!T||t&1)&&F!==(F=e[0].name+"")&&G(K,F),t&6&&(H=I(e[2]),w=we(w,t,_e,1,e,H,me,R,je,Ae,null,Me)),t&6&&(q=I(e[2]),De(),p=we(p,t,pe,1,e,q,be,U,Pe,Ce,null,ye),Re())},i(e){if(!T){Q(M.$$.fragment,e),Q(L.$$.fragment,e);for(let t=0;t<q.length;t+=1)Q(p[t]);T=!0}},o(e){J(M.$$.fragment,e),J(L.$$.fragment,e);for(let t=0;t<p.length;t+=1)J(p[t]);T=!1},d(e){e&&(u(s),u(i),u(f),u(V),u(W),u(S),u(X),u(A),u(Y),u(D),u(x),u(C),u(le),u(P),u(se),u(B)),ne(M,e),ne(L);for(let t=0;t<w.length;t+=1)w[t].d();for(let t=0;t<p.length;t+=1)p[t].d()}}}function He(o,s,l){let a,{collection:h}=s,b=200,i=[],f={},m=!1;v();async function v(){l(5,m=!0);try{l(4,f=await ae.collection(h.name).listAuthMethods())}catch(g){ae.error(g)}l(5,m=!1)}const $=g=>l(1,b=g.code);return o.$$set=g=>{"collection"in g&&l(0,h=g.collection)},o.$$.update=()=>{o.$$.dirty&48&&l(2,i=[{code:200,body:m?"...":JSON.stringify(f,null,2)},{code:404,body:`
                {
                  "status": 404,
                  "message": "Missing collection context.",
                  "data": {}
                }
            `}])},l(3,a=Ue.getApiExampleUrl(ae.baseURL)),[h,b,i,a,f,m,$]}class Qe extends Be{constructor(s){super(),Te(this,s,He,Fe,Le,{collection:0})}}export{Qe as default};
