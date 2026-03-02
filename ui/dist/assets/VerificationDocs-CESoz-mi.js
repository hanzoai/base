import{S as le,i as ne,s as ie,X as F,j as h,t as U,a as L,I as X,Z as x,_ as Te,C as ee,$ as Ce,D as te,n as p,o as u,u as m,v as y,A as N,w as v,l as K,p as se,W as qe,d as Z,m as G,c as Q,V as Ve,Y as fe,J as Ae,b as Ie,a0 as ue}from"./index-D7eFJ7oc.js";function de(o,t,e){const s=o.slice();return s[4]=t[e],s}function me(o,t,e){const s=o.slice();return s[4]=t[e],s}function _e(o,t){let e,s=t[4].code+"",f,c,r,a;function d(){return t[3](t[4])}return{key:o,first:null,c(){e=m("button"),f=N(s),c=y(),v(e,"class","tab-item"),K(e,"active",t[1]===t[4].code),this.first=e},m(g,C){p(g,e,C),u(e,f),u(e,c),r||(a=se(e,"click",d),r=!0)},p(g,C){t=g,C&4&&s!==(s=t[4].code+"")&&X(f,s),C&6&&K(e,"active",t[1]===t[4].code)},d(g){g&&h(e),r=!1,a()}}}function be(o,t){let e,s,f,c;return s=new qe({props:{content:t[4].body}}),{key:o,first:null,c(){e=m("div"),Q(s.$$.fragment),f=y(),v(e,"class","tab-item"),K(e,"active",t[1]===t[4].code),this.first=e},m(r,a){p(r,e,a),G(s,e,null),u(e,f),c=!0},p(r,a){t=r;const d={};a&4&&(d.content=t[4].body),s.$set(d),(!c||a&6)&&K(e,"active",t[1]===t[4].code)},i(r){c||(L(s.$$.fragment,r),c=!0)},o(r){U(s.$$.fragment,r),c=!1},d(r){r&&h(e),Z(s)}}}function Re(o){let t,e,s,f,c,r,a,d=o[0].name+"",g,C,D,R,H,B,O,E,S,q,V,$=[],z=new Map,j,I,_=[],T=new Map,A,b=F(o[2]);const W=l=>l[4].code;for(let l=0;l<b.length;l+=1){let i=me(o,b,l),n=W(i);z.set(n,$[l]=_e(n,i))}let M=F(o[2]);const J=l=>l[4].code;for(let l=0;l<M.length;l+=1){let i=de(o,M,l),n=J(i);T.set(n,_[l]=be(n,i))}return{c(){t=m("div"),e=m("strong"),e.textContent="POST",s=y(),f=m("div"),c=m("p"),r=N("/api/collections/"),a=m("strong"),g=N(d),C=N("/confirm-verification"),D=y(),R=m("div"),R.textContent="Body Parameters",H=y(),B=m("table"),B.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>token</span></div></td> <td><span class="label">String</span></td> <td>The token from the verification request email.</td></tr></tbody>',O=y(),E=m("div"),E.textContent="Responses",S=y(),q=m("div"),V=m("div");for(let l=0;l<$.length;l+=1)$[l].c();j=y(),I=m("div");for(let l=0;l<_.length;l+=1)_[l].c();v(e,"class","label label-primary"),v(f,"class","content"),v(t,"class","alert alert-success"),v(R,"class","section-title"),v(B,"class","table-compact table-border m-b-base"),v(E,"class","section-title"),v(V,"class","tabs-header compact combined left"),v(I,"class","tabs-content"),v(q,"class","tabs")},m(l,i){p(l,t,i),u(t,e),u(t,s),u(t,f),u(f,c),u(c,r),u(c,a),u(a,g),u(c,C),p(l,D,i),p(l,R,i),p(l,H,i),p(l,B,i),p(l,O,i),p(l,E,i),p(l,S,i),p(l,q,i),u(q,V);for(let n=0;n<$.length;n+=1)$[n]&&$[n].m(V,null);u(q,j),u(q,I);for(let n=0;n<_.length;n+=1)_[n]&&_[n].m(I,null);A=!0},p(l,[i]){(!A||i&1)&&d!==(d=l[0].name+"")&&X(g,d),i&6&&(b=F(l[2]),$=x($,i,W,1,l,b,z,V,Te,_e,null,me)),i&6&&(M=F(l[2]),ee(),_=x(_,i,J,1,l,M,T,I,Ce,be,null,de),te())},i(l){if(!A){for(let i=0;i<M.length;i+=1)L(_[i]);A=!0}},o(l){for(let i=0;i<_.length;i+=1)U(_[i]);A=!1},d(l){l&&(h(t),h(D),h(R),h(H),h(B),h(O),h(E),h(S),h(q));for(let i=0;i<$.length;i+=1)$[i].d();for(let i=0;i<_.length;i+=1)_[i].d()}}}function Be(o,t,e){let{collection:s}=t,f=204,c=[];const r=a=>e(1,f=a.code);return o.$$set=a=>{"collection"in a&&e(0,s=a.collection)},e(2,c=[{code:204,body:"null"},{code:400,body:`
                {
                  "status": 400,
                  "message": "An error occurred while validating the submitted data.",
                  "data": {
                    "token": {
                      "code": "validation_required",
                      "message": "Missing required value."
                    }
                  }
                }
            `}]),[s,f,c,r]}class Oe extends le{constructor(t){super(),ne(this,t,Be,Re,ie,{collection:0})}}function he(o,t,e){const s=o.slice();return s[4]=t[e],s}function pe(o,t,e){const s=o.slice();return s[4]=t[e],s}function ve(o,t){let e,s=t[4].code+"",f,c,r,a;function d(){return t[3](t[4])}return{key:o,first:null,c(){e=m("button"),f=N(s),c=y(),v(e,"class","tab-item"),K(e,"active",t[1]===t[4].code),this.first=e},m(g,C){p(g,e,C),u(e,f),u(e,c),r||(a=se(e,"click",d),r=!0)},p(g,C){t=g,C&4&&s!==(s=t[4].code+"")&&X(f,s),C&6&&K(e,"active",t[1]===t[4].code)},d(g){g&&h(e),r=!1,a()}}}function ge(o,t){let e,s,f,c;return s=new qe({props:{content:t[4].body}}),{key:o,first:null,c(){e=m("div"),Q(s.$$.fragment),f=y(),v(e,"class","tab-item"),K(e,"active",t[1]===t[4].code),this.first=e},m(r,a){p(r,e,a),G(s,e,null),u(e,f),c=!0},p(r,a){t=r;const d={};a&4&&(d.content=t[4].body),s.$set(d),(!c||a&6)&&K(e,"active",t[1]===t[4].code)},i(r){c||(L(s.$$.fragment,r),c=!0)},o(r){U(s.$$.fragment,r),c=!1},d(r){r&&h(e),Z(s)}}}function Ee(o){let t,e,s,f,c,r,a,d=o[0].name+"",g,C,D,R,H,B,O,E,S,q,V,$=[],z=new Map,j,I,_=[],T=new Map,A,b=F(o[2]);const W=l=>l[4].code;for(let l=0;l<b.length;l+=1){let i=pe(o,b,l),n=W(i);z.set(n,$[l]=ve(n,i))}let M=F(o[2]);const J=l=>l[4].code;for(let l=0;l<M.length;l+=1){let i=he(o,M,l),n=J(i);T.set(n,_[l]=ge(n,i))}return{c(){t=m("div"),e=m("strong"),e.textContent="POST",s=y(),f=m("div"),c=m("p"),r=N("/api/collections/"),a=m("strong"),g=N(d),C=N("/request-verification"),D=y(),R=m("div"),R.textContent="Body Parameters",H=y(),B=m("table"),B.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>email</span></div></td> <td><span class="label">String</span></td> <td>The auth record email address to send the verification request (if exists).</td></tr></tbody>',O=y(),E=m("div"),E.textContent="Responses",S=y(),q=m("div"),V=m("div");for(let l=0;l<$.length;l+=1)$[l].c();j=y(),I=m("div");for(let l=0;l<_.length;l+=1)_[l].c();v(e,"class","label label-primary"),v(f,"class","content"),v(t,"class","alert alert-success"),v(R,"class","section-title"),v(B,"class","table-compact table-border m-b-base"),v(E,"class","section-title"),v(V,"class","tabs-header compact combined left"),v(I,"class","tabs-content"),v(q,"class","tabs")},m(l,i){p(l,t,i),u(t,e),u(t,s),u(t,f),u(f,c),u(c,r),u(c,a),u(a,g),u(c,C),p(l,D,i),p(l,R,i),p(l,H,i),p(l,B,i),p(l,O,i),p(l,E,i),p(l,S,i),p(l,q,i),u(q,V);for(let n=0;n<$.length;n+=1)$[n]&&$[n].m(V,null);u(q,j),u(q,I);for(let n=0;n<_.length;n+=1)_[n]&&_[n].m(I,null);A=!0},p(l,[i]){(!A||i&1)&&d!==(d=l[0].name+"")&&X(g,d),i&6&&(b=F(l[2]),$=x($,i,W,1,l,b,z,V,Te,ve,null,pe)),i&6&&(M=F(l[2]),ee(),_=x(_,i,J,1,l,M,T,I,Ce,ge,null,he),te())},i(l){if(!A){for(let i=0;i<M.length;i+=1)L(_[i]);A=!0}},o(l){for(let i=0;i<_.length;i+=1)U(_[i]);A=!1},d(l){l&&(h(t),h(D),h(R),h(H),h(B),h(O),h(E),h(S),h(q));for(let i=0;i<$.length;i+=1)$[i].d();for(let i=0;i<_.length;i+=1)_[i].d()}}}function Me(o,t,e){let{collection:s}=t,f=204,c=[];const r=a=>e(1,f=a.code);return o.$$set=a=>{"collection"in a&&e(0,s=a.collection)},e(2,c=[{code:204,body:"null"},{code:400,body:`
                {
                  "status": 400,
                  "message": "An error occurred while validating the submitted data.",
                  "data": {
                    "email": {
                      "code": "validation_required",
                      "message": "Missing required value."
                    }
                  }
                }
            `}]),[s,f,c,r]}class Ne extends le{constructor(t){super(),ne(this,t,Me,Ee,ie,{collection:0})}}function ke(o,t,e){const s=o.slice();return s[5]=t[e],s[7]=e,s}function $e(o,t,e){const s=o.slice();return s[5]=t[e],s[7]=e,s}function we(o){let t,e,s,f,c;function r(){return o[4](o[7])}return{c(){t=m("button"),e=m("div"),e.textContent=`${o[5].title}`,s=y(),v(e,"class","txt"),v(t,"class","tab-item"),K(t,"active",o[1]==o[7])},m(a,d){p(a,t,d),u(t,e),u(t,s),f||(c=se(t,"click",r),f=!0)},p(a,d){o=a,d&2&&K(t,"active",o[1]==o[7])},d(a){a&&h(t),f=!1,c()}}}function ye(o){let t,e,s,f;var c=o[5].component;function r(a,d){return{props:{collection:a[0]}}}return c&&(e=ue(c,r(o))),{c(){t=m("div"),e&&Q(e.$$.fragment),s=y(),v(t,"class","tab-item"),K(t,"active",o[1]==o[7])},m(a,d){p(a,t,d),e&&G(e,t,null),u(t,s),f=!0},p(a,d){if(c!==(c=a[5].component)){if(e){ee();const g=e;U(g.$$.fragment,1,0,()=>{Z(g,1)}),te()}c?(e=ue(c,r(a)),Q(e.$$.fragment),L(e.$$.fragment,1),G(e,t,s)):e=null}else if(c){const g={};d&1&&(g.collection=a[0]),e.$set(g)}(!f||d&2)&&K(t,"active",a[1]==a[7])},i(a){f||(e&&L(e.$$.fragment,a),f=!0)},o(a){e&&U(e.$$.fragment,a),f=!1},d(a){a&&h(t),e&&Z(e)}}}function Se(o){var M,J,l,i;let t,e,s=o[0].name+"",f,c,r,a,d,g,C,D=o[0].name+"",R,H,B,O,E,S,q,V,$,z,j,I;O=new Ve({props:{js:`
        import Base from 'base';

        const base = new Base('${o[2]}');

        ...

        await base.collection('${(M=o[0])==null?void 0:M.name}').requestVerification('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        await base.collection('${(J=o[0])==null?void 0:J.name}').confirmVerification('VERIFICATION_TOKEN');
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${o[2]}');

        ...

        await base.collection('${(l=o[0])==null?void 0:l.name}').requestVerification('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        await base.collection('${(i=o[0])==null?void 0:i.name}').confirmVerification('VERIFICATION_TOKEN');
    `}});let _=F(o[3]),T=[];for(let n=0;n<_.length;n+=1)T[n]=we($e(o,_,n));let A=F(o[3]),b=[];for(let n=0;n<A.length;n+=1)b[n]=ye(ke(o,A,n));const W=n=>U(b[n],1,1,()=>{b[n]=null});return{c(){t=m("h3"),e=N("Account verification ("),f=N(s),c=N(")"),r=y(),a=m("div"),d=m("p"),g=N("Sends "),C=m("strong"),R=N(D),H=N(" account verification request."),B=y(),Q(O.$$.fragment),E=y(),S=m("h6"),S.textContent="API details",q=y(),V=m("div"),$=m("div");for(let n=0;n<T.length;n+=1)T[n].c();z=y(),j=m("div");for(let n=0;n<b.length;n+=1)b[n].c();v(t,"class","m-b-sm"),v(a,"class","content txt-lg m-b-sm"),v(S,"class","m-b-xs"),v($,"class","tabs-header compact"),v(j,"class","tabs-content"),v(V,"class","tabs")},m(n,w){p(n,t,w),u(t,e),u(t,f),u(t,c),p(n,r,w),p(n,a,w),u(a,d),u(d,g),u(d,C),u(C,R),u(d,H),p(n,B,w),G(O,n,w),p(n,E,w),p(n,S,w),p(n,q,w),p(n,V,w),u(V,$);for(let P=0;P<T.length;P+=1)T[P]&&T[P].m($,null);u(V,z),u(V,j);for(let P=0;P<b.length;P+=1)b[P]&&b[P].m(j,null);I=!0},p(n,[w]){var oe,ae,ce,re;(!I||w&1)&&s!==(s=n[0].name+"")&&X(f,s),(!I||w&1)&&D!==(D=n[0].name+"")&&X(R,D);const P={};if(w&5&&(P.js=`
        import Base from 'base';

        const base = new Base('${n[2]}');

        ...

        await base.collection('${(oe=n[0])==null?void 0:oe.name}').requestVerification('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        await base.collection('${(ae=n[0])==null?void 0:ae.name}').confirmVerification('VERIFICATION_TOKEN');
    `),w&5&&(P.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${n[2]}');

        ...

        await base.collection('${(ce=n[0])==null?void 0:ce.name}').requestVerification('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        await base.collection('${(re=n[0])==null?void 0:re.name}').confirmVerification('VERIFICATION_TOKEN');
    `),O.$set(P),w&10){_=F(n[3]);let k;for(k=0;k<_.length;k+=1){const Y=$e(n,_,k);T[k]?T[k].p(Y,w):(T[k]=we(Y),T[k].c(),T[k].m($,null))}for(;k<T.length;k+=1)T[k].d(1);T.length=_.length}if(w&11){A=F(n[3]);let k;for(k=0;k<A.length;k+=1){const Y=ke(n,A,k);b[k]?(b[k].p(Y,w),L(b[k],1)):(b[k]=ye(Y),b[k].c(),L(b[k],1),b[k].m(j,null))}for(ee(),k=A.length;k<b.length;k+=1)W(k);te()}},i(n){if(!I){L(O.$$.fragment,n);for(let w=0;w<A.length;w+=1)L(b[w]);I=!0}},o(n){U(O.$$.fragment,n),b=b.filter(Boolean);for(let w=0;w<b.length;w+=1)U(b[w]);I=!1},d(n){n&&(h(t),h(r),h(a),h(B),h(E),h(S),h(q),h(V)),Z(O,n),fe(T,n),fe(b,n)}}}function Pe(o,t,e){let s,{collection:f}=t;const c=[{title:"Request verification",component:Ne},{title:"Confirm verification",component:Oe}];let r=0;const a=d=>e(1,r=d);return o.$$set=d=>{"collection"in d&&e(0,f=d.collection)},e(2,s=Ae.getApiExampleUrl(Ie.baseURL)),[f,r,s,c,a]}class Fe extends le{constructor(t){super(),ne(this,t,Pe,Se,ie,{collection:0})}}export{Fe as default};
