import{S as se,i as ne,s as ae,X as j,j as b,t as V,a as J,I as Y,Z as ee,_ as Pe,C as te,$ as Te,D as le,n as v,o as u,u as m,v as y,A as D,w as g,l as H,p as oe,W as Ee,d as G,m as Q,c as x,V as Ce,Y as fe,J as qe,b as Oe,a0 as me}from"./index-DQaqjr2E.js";function pe(a,t,e){const n=a.slice();return n[4]=t[e],n}function _e(a,t,e){const n=a.slice();return n[4]=t[e],n}function he(a,t){let e,n=t[4].code+"",d,c,r,o;function f(){return t[3](t[4])}return{key:a,first:null,c(){e=m("button"),d=D(n),c=y(),g(e,"class","tab-item"),H(e,"active",t[1]===t[4].code),this.first=e},m(k,P){v(k,e,P),u(e,d),u(e,c),r||(o=oe(e,"click",f),r=!0)},p(k,P){t=k,P&4&&n!==(n=t[4].code+"")&&Y(d,n),P&6&&H(e,"active",t[1]===t[4].code)},d(k){k&&b(e),r=!1,o()}}}function be(a,t){let e,n,d,c;return n=new Ee({props:{content:t[4].body}}),{key:a,first:null,c(){e=m("div"),x(n.$$.fragment),d=y(),g(e,"class","tab-item"),H(e,"active",t[1]===t[4].code),this.first=e},m(r,o){v(r,e,o),Q(n,e,null),u(e,d),c=!0},p(r,o){t=r;const f={};o&4&&(f.content=t[4].body),n.$set(f),(!c||o&6)&&H(e,"active",t[1]===t[4].code)},i(r){c||(J(n.$$.fragment,r),c=!0)},o(r){V(n.$$.fragment,r),c=!1},d(r){r&&b(e),G(n)}}}function Ae(a){let t,e,n,d,c,r,o,f=a[0].name+"",k,P,F,q,z,W,L,O,A,T,C,R=[],M=new Map,U,N,h=[],K=new Map,E,S=j(a[2]);const B=l=>l[4].code;for(let l=0;l<S.length;l+=1){let s=_e(a,S,l),_=B(s);M.set(_,R[l]=he(_,s))}let p=j(a[2]);const X=l=>l[4].code;for(let l=0;l<p.length;l+=1){let s=pe(a,p,l),_=X(s);K.set(_,h[l]=be(_,s))}return{c(){t=m("div"),e=m("strong"),e.textContent="POST",n=y(),d=m("div"),c=m("p"),r=D("/api/collections/"),o=m("strong"),k=D(f),P=D("/confirm-password-reset"),F=y(),q=m("div"),q.textContent="Body Parameters",z=y(),W=m("table"),W.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>token</span></div></td> <td><span class="label">String</span></td> <td>The token from the password reset request email.</td></tr> <tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>password</span></div></td> <td><span class="label">String</span></td> <td>The new password to set.</td></tr> <tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>passwordConfirm</span></div></td> <td><span class="label">String</span></td> <td>The new password confirmation.</td></tr></tbody>',L=y(),O=m("div"),O.textContent="Responses",A=y(),T=m("div"),C=m("div");for(let l=0;l<R.length;l+=1)R[l].c();U=y(),N=m("div");for(let l=0;l<h.length;l+=1)h[l].c();g(e,"class","label label-primary"),g(d,"class","content"),g(t,"class","alert alert-success"),g(q,"class","section-title"),g(W,"class","table-compact table-border m-b-base"),g(O,"class","section-title"),g(C,"class","tabs-header compact combined left"),g(N,"class","tabs-content"),g(T,"class","tabs")},m(l,s){v(l,t,s),u(t,e),u(t,n),u(t,d),u(d,c),u(c,r),u(c,o),u(o,k),u(c,P),v(l,F,s),v(l,q,s),v(l,z,s),v(l,W,s),v(l,L,s),v(l,O,s),v(l,A,s),v(l,T,s),u(T,C);for(let _=0;_<R.length;_+=1)R[_]&&R[_].m(C,null);u(T,U),u(T,N);for(let _=0;_<h.length;_+=1)h[_]&&h[_].m(N,null);E=!0},p(l,[s]){(!E||s&1)&&f!==(f=l[0].name+"")&&Y(k,f),s&6&&(S=j(l[2]),R=ee(R,s,B,1,l,S,M,C,Pe,he,null,_e)),s&6&&(p=j(l[2]),te(),h=ee(h,s,X,1,l,p,K,N,Te,be,null,pe),le())},i(l){if(!E){for(let s=0;s<p.length;s+=1)J(h[s]);E=!0}},o(l){for(let s=0;s<h.length;s+=1)V(h[s]);E=!1},d(l){l&&(b(t),b(F),b(q),b(z),b(W),b(L),b(O),b(A),b(T));for(let s=0;s<R.length;s+=1)R[s].d();for(let s=0;s<h.length;s+=1)h[s].d()}}}function We(a,t,e){let{collection:n}=t,d=204,c=[];const r=o=>e(1,d=o.code);return a.$$set=o=>{"collection"in o&&e(0,n=o.collection)},e(2,c=[{code:204,body:"null"},{code:400,body:`
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
            `}]),[n,d,c,r]}class Ne extends se{constructor(t){super(),ne(this,t,We,Ae,ae,{collection:0})}}function ve(a,t,e){const n=a.slice();return n[4]=t[e],n}function ge(a,t,e){const n=a.slice();return n[4]=t[e],n}function ke(a,t){let e,n=t[4].code+"",d,c,r,o;function f(){return t[3](t[4])}return{key:a,first:null,c(){e=m("button"),d=D(n),c=y(),g(e,"class","tab-item"),H(e,"active",t[1]===t[4].code),this.first=e},m(k,P){v(k,e,P),u(e,d),u(e,c),r||(o=oe(e,"click",f),r=!0)},p(k,P){t=k,P&4&&n!==(n=t[4].code+"")&&Y(d,n),P&6&&H(e,"active",t[1]===t[4].code)},d(k){k&&b(e),r=!1,o()}}}function we(a,t){let e,n,d,c;return n=new Ee({props:{content:t[4].body}}),{key:a,first:null,c(){e=m("div"),x(n.$$.fragment),d=y(),g(e,"class","tab-item"),H(e,"active",t[1]===t[4].code),this.first=e},m(r,o){v(r,e,o),Q(n,e,null),u(e,d),c=!0},p(r,o){t=r;const f={};o&4&&(f.content=t[4].body),n.$set(f),(!c||o&6)&&H(e,"active",t[1]===t[4].code)},i(r){c||(J(n.$$.fragment,r),c=!0)},o(r){V(n.$$.fragment,r),c=!1},d(r){r&&b(e),G(n)}}}function De(a){let t,e,n,d,c,r,o,f=a[0].name+"",k,P,F,q,z,W,L,O,A,T,C,R=[],M=new Map,U,N,h=[],K=new Map,E,S=j(a[2]);const B=l=>l[4].code;for(let l=0;l<S.length;l+=1){let s=ge(a,S,l),_=B(s);M.set(_,R[l]=ke(_,s))}let p=j(a[2]);const X=l=>l[4].code;for(let l=0;l<p.length;l+=1){let s=ve(a,p,l),_=X(s);K.set(_,h[l]=we(_,s))}return{c(){t=m("div"),e=m("strong"),e.textContent="POST",n=y(),d=m("div"),c=m("p"),r=D("/api/collections/"),o=m("strong"),k=D(f),P=D("/request-password-reset"),F=y(),q=m("div"),q.textContent="Body Parameters",z=y(),W=m("table"),W.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>email</span></div></td> <td><span class="label">String</span></td> <td>The auth record email address to send the password reset request (if exists).</td></tr></tbody>',L=y(),O=m("div"),O.textContent="Responses",A=y(),T=m("div"),C=m("div");for(let l=0;l<R.length;l+=1)R[l].c();U=y(),N=m("div");for(let l=0;l<h.length;l+=1)h[l].c();g(e,"class","label label-primary"),g(d,"class","content"),g(t,"class","alert alert-success"),g(q,"class","section-title"),g(W,"class","table-compact table-border m-b-base"),g(O,"class","section-title"),g(C,"class","tabs-header compact combined left"),g(N,"class","tabs-content"),g(T,"class","tabs")},m(l,s){v(l,t,s),u(t,e),u(t,n),u(t,d),u(d,c),u(c,r),u(c,o),u(o,k),u(c,P),v(l,F,s),v(l,q,s),v(l,z,s),v(l,W,s),v(l,L,s),v(l,O,s),v(l,A,s),v(l,T,s),u(T,C);for(let _=0;_<R.length;_+=1)R[_]&&R[_].m(C,null);u(T,U),u(T,N);for(let _=0;_<h.length;_+=1)h[_]&&h[_].m(N,null);E=!0},p(l,[s]){(!E||s&1)&&f!==(f=l[0].name+"")&&Y(k,f),s&6&&(S=j(l[2]),R=ee(R,s,B,1,l,S,M,C,Pe,ke,null,ge)),s&6&&(p=j(l[2]),te(),h=ee(h,s,X,1,l,p,K,N,Te,we,null,ve),le())},i(l){if(!E){for(let s=0;s<p.length;s+=1)J(h[s]);E=!0}},o(l){for(let s=0;s<h.length;s+=1)V(h[s]);E=!1},d(l){l&&(b(t),b(F),b(q),b(z),b(W),b(L),b(O),b(A),b(T));for(let s=0;s<R.length;s+=1)R[s].d();for(let s=0;s<h.length;s+=1)h[s].d()}}}function Me(a,t,e){let{collection:n}=t,d=204,c=[];const r=o=>e(1,d=o.code);return a.$$set=o=>{"collection"in o&&e(0,n=o.collection)},e(2,c=[{code:204,body:"null"},{code:400,body:`
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
            `}]),[n,d,c,r]}class Be extends se{constructor(t){super(),ne(this,t,Me,De,ae,{collection:0})}}function $e(a,t,e){const n=a.slice();return n[5]=t[e],n[7]=e,n}function Re(a,t,e){const n=a.slice();return n[5]=t[e],n[7]=e,n}function Se(a){let t,e,n,d,c;function r(){return a[4](a[7])}return{c(){t=m("button"),e=m("div"),e.textContent=`${a[5].title}`,n=y(),g(e,"class","txt"),g(t,"class","tab-item"),H(t,"active",a[1]==a[7])},m(o,f){v(o,t,f),u(t,e),u(t,n),d||(c=oe(t,"click",r),d=!0)},p(o,f){a=o,f&2&&H(t,"active",a[1]==a[7])},d(o){o&&b(t),d=!1,c()}}}function ye(a){let t,e,n,d;var c=a[5].component;function r(o,f){return{props:{collection:o[0]}}}return c&&(e=me(c,r(a))),{c(){t=m("div"),e&&x(e.$$.fragment),n=y(),g(t,"class","tab-item"),H(t,"active",a[1]==a[7])},m(o,f){v(o,t,f),e&&Q(e,t,null),u(t,n),d=!0},p(o,f){if(c!==(c=o[5].component)){if(e){te();const k=e;V(k.$$.fragment,1,0,()=>{G(k,1)}),le()}c?(e=me(c,r(o)),x(e.$$.fragment),J(e.$$.fragment,1),Q(e,t,n)):e=null}else if(c){const k={};f&1&&(k.collection=o[0]),e.$set(k)}(!d||f&2)&&H(t,"active",o[1]==o[7])},i(o){d||(e&&J(e.$$.fragment,o),d=!0)},o(o){e&&V(e.$$.fragment,o),d=!1},d(o){o&&b(t),e&&G(e)}}}function Ie(a){var l,s,_,ie;let t,e,n=a[0].name+"",d,c,r,o,f,k,P,F=a[0].name+"",q,z,W,L,O,A,T,C,R,M,U,N,h,K;A=new Ce({props:{js:`
        import Base from 'base';

        const base = new Base('${a[2]}');

        ...

        await base.collection('${(l=a[0])==null?void 0:l.name}').requestPasswordReset('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(s=a[0])==null?void 0:s.name}').confirmPasswordReset(
            'RESET_TOKEN',
            'NEW_PASSWORD',
            'NEW_PASSWORD_CONFIRM',
        );
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${a[2]}');

        ...

        await base.collection('${(_=a[0])==null?void 0:_.name}').requestPasswordReset('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(ie=a[0])==null?void 0:ie.name}').confirmPasswordReset(
          'RESET_TOKEN',
          'NEW_PASSWORD',
          'NEW_PASSWORD_CONFIRM',
        );
    `}});let E=j(a[3]),S=[];for(let i=0;i<E.length;i+=1)S[i]=Se(Re(a,E,i));let B=j(a[3]),p=[];for(let i=0;i<B.length;i+=1)p[i]=ye($e(a,B,i));const X=i=>V(p[i],1,1,()=>{p[i]=null});return{c(){t=m("h3"),e=D("Password reset ("),d=D(n),c=D(")"),r=y(),o=m("div"),f=m("p"),k=D("Sends "),P=m("strong"),q=D(F),z=D(" password reset email request."),W=y(),L=m("p"),L.textContent=`On successful password reset all previously issued auth tokens for the specific record will be
        automatically invalidated.`,O=y(),x(A.$$.fragment),T=y(),C=m("h6"),C.textContent="API details",R=y(),M=m("div"),U=m("div");for(let i=0;i<S.length;i+=1)S[i].c();N=y(),h=m("div");for(let i=0;i<p.length;i+=1)p[i].c();g(t,"class","m-b-sm"),g(o,"class","content txt-lg m-b-sm"),g(C,"class","m-b-xs"),g(U,"class","tabs-header compact"),g(h,"class","tabs-content"),g(M,"class","tabs")},m(i,$){v(i,t,$),u(t,e),u(t,d),u(t,c),v(i,r,$),v(i,o,$),u(o,f),u(f,k),u(f,P),u(P,q),u(f,z),u(o,W),u(o,L),v(i,O,$),Q(A,i,$),v(i,T,$),v(i,C,$),v(i,R,$),v(i,M,$),u(M,U);for(let I=0;I<S.length;I+=1)S[I]&&S[I].m(U,null);u(M,N),u(M,h);for(let I=0;I<p.length;I+=1)p[I]&&p[I].m(h,null);K=!0},p(i,[$]){var ce,re,de,ue;(!K||$&1)&&n!==(n=i[0].name+"")&&Y(d,n),(!K||$&1)&&F!==(F=i[0].name+"")&&Y(q,F);const I={};if($&5&&(I.js=`
        import Base from 'base';

        const base = new Base('${i[2]}');

        ...

        await base.collection('${(ce=i[0])==null?void 0:ce.name}').requestPasswordReset('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(re=i[0])==null?void 0:re.name}').confirmPasswordReset(
            'RESET_TOKEN',
            'NEW_PASSWORD',
            'NEW_PASSWORD_CONFIRM',
        );
    `),$&5&&(I.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${i[2]}');

        ...

        await base.collection('${(de=i[0])==null?void 0:de.name}').requestPasswordReset('test@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(ue=i[0])==null?void 0:ue.name}').confirmPasswordReset(
          'RESET_TOKEN',
          'NEW_PASSWORD',
          'NEW_PASSWORD_CONFIRM',
        );
    `),A.$set(I),$&10){E=j(i[3]);let w;for(w=0;w<E.length;w+=1){const Z=Re(i,E,w);S[w]?S[w].p(Z,$):(S[w]=Se(Z),S[w].c(),S[w].m(U,null))}for(;w<S.length;w+=1)S[w].d(1);S.length=E.length}if($&11){B=j(i[3]);let w;for(w=0;w<B.length;w+=1){const Z=$e(i,B,w);p[w]?(p[w].p(Z,$),J(p[w],1)):(p[w]=ye(Z),p[w].c(),J(p[w],1),p[w].m(h,null))}for(te(),w=B.length;w<p.length;w+=1)X(w);le()}},i(i){if(!K){J(A.$$.fragment,i);for(let $=0;$<B.length;$+=1)J(p[$]);K=!0}},o(i){V(A.$$.fragment,i),p=p.filter(Boolean);for(let $=0;$<p.length;$+=1)V(p[$]);K=!1},d(i){i&&(b(t),b(r),b(o),b(O),b(T),b(C),b(R),b(M)),G(A,i),fe(S,i),fe(p,i)}}}function Fe(a,t,e){let n,{collection:d}=t;const c=[{title:"Request password reset",component:Be},{title:"Confirm password reset",component:Ne}];let r=0;const o=f=>e(1,r=f);return a.$$set=f=>{"collection"in f&&e(0,d=f.collection)},e(2,n=qe.getApiExampleUrl(Oe.baseURL)),[d,r,n,c,o]}class je extends se{constructor(t){super(),ne(this,t,Fe,Ie,ae,{collection:0})}}export{je as default};
