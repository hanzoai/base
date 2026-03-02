import{S as se,i as ie,s as oe,X as z,j as g,t as X,a as V,I as F,Z as le,_ as Se,C as ae,$ as Oe,D as ne,n as v,o as u,u as p,v as y,A as U,w as b,l as K,p as ce,W as Pe,d as x,m as ee,c as te,V as Me,Y as _e,J as Be,b as De,a0 as be}from"./index-DQaqjr2E.js";function ge(a,e,t){const l=a.slice();return l[4]=e[t],l}function ve(a,e,t){const l=a.slice();return l[4]=e[t],l}function ke(a,e){let t,l=e[4].code+"",d,o,r,n;function m(){return e[3](e[4])}return{key:a,first:null,c(){t=p("button"),d=U(l),o=y(),b(t,"class","tab-item"),K(t,"active",e[1]===e[4].code),this.first=t},m(k,q){v(k,t,q),u(t,d),u(t,o),r||(n=ce(t,"click",m),r=!0)},p(k,q){e=k,q&4&&l!==(l=e[4].code+"")&&F(d,l),q&6&&K(t,"active",e[1]===e[4].code)},d(k){k&&g(t),r=!1,n()}}}function $e(a,e){let t,l,d,o;return l=new Pe({props:{content:e[4].body}}),{key:a,first:null,c(){t=p("div"),te(l.$$.fragment),d=y(),b(t,"class","tab-item"),K(t,"active",e[1]===e[4].code),this.first=t},m(r,n){v(r,t,n),ee(l,t,null),u(t,d),o=!0},p(r,n){e=r;const m={};n&4&&(m.content=e[4].body),l.$set(m),(!o||n&6)&&K(t,"active",e[1]===e[4].code)},i(r){o||(V(l.$$.fragment,r),o=!0)},o(r){X(l.$$.fragment,r),o=!1},d(r){r&&g(t),x(l)}}}function Ne(a){let e,t,l,d,o,r,n,m=a[0].name+"",k,q,Y,H,J,L,G,B,D,O,N,A=[],P=new Map,R,j,T=[],W=new Map,w,E=z(a[2]);const M=c=>c[4].code;for(let c=0;c<E.length;c+=1){let f=ve(a,E,c),s=M(f);P.set(s,A[c]=ke(s,f))}let _=z(a[2]);const Z=c=>c[4].code;for(let c=0;c<_.length;c+=1){let f=ge(a,_,c),s=Z(f);W.set(s,T[c]=$e(s,f))}return{c(){e=p("div"),t=p("strong"),t.textContent="POST",l=y(),d=p("div"),o=p("p"),r=U("/api/collections/"),n=p("strong"),k=U(m),q=U("/confirm-email-change"),Y=y(),H=p("div"),H.textContent="Body Parameters",J=y(),L=p("table"),L.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>token</span></div></td> <td><span class="label">String</span></td> <td>The token from the change email request email.</td></tr> <tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>password</span></div></td> <td><span class="label">String</span></td> <td>The account password to confirm the email change.</td></tr></tbody>',G=y(),B=p("div"),B.textContent="Responses",D=y(),O=p("div"),N=p("div");for(let c=0;c<A.length;c+=1)A[c].c();R=y(),j=p("div");for(let c=0;c<T.length;c+=1)T[c].c();b(t,"class","label label-primary"),b(d,"class","content"),b(e,"class","alert alert-success"),b(H,"class","section-title"),b(L,"class","table-compact table-border m-b-base"),b(B,"class","section-title"),b(N,"class","tabs-header compact combined left"),b(j,"class","tabs-content"),b(O,"class","tabs")},m(c,f){v(c,e,f),u(e,t),u(e,l),u(e,d),u(d,o),u(o,r),u(o,n),u(n,k),u(o,q),v(c,Y,f),v(c,H,f),v(c,J,f),v(c,L,f),v(c,G,f),v(c,B,f),v(c,D,f),v(c,O,f),u(O,N);for(let s=0;s<A.length;s+=1)A[s]&&A[s].m(N,null);u(O,R),u(O,j);for(let s=0;s<T.length;s+=1)T[s]&&T[s].m(j,null);w=!0},p(c,[f]){(!w||f&1)&&m!==(m=c[0].name+"")&&F(k,m),f&6&&(E=z(c[2]),A=le(A,f,M,1,c,E,P,N,Se,ke,null,ve)),f&6&&(_=z(c[2]),ae(),T=le(T,f,Z,1,c,_,W,j,Oe,$e,null,ge),ne())},i(c){if(!w){for(let f=0;f<_.length;f+=1)V(T[f]);w=!0}},o(c){for(let f=0;f<T.length;f+=1)X(T[f]);w=!1},d(c){c&&(g(e),g(Y),g(H),g(J),g(L),g(G),g(B),g(D),g(O));for(let f=0;f<A.length;f+=1)A[f].d();for(let f=0;f<T.length;f+=1)T[f].d()}}}function We(a,e,t){let{collection:l}=e,d=204,o=[];const r=n=>t(1,d=n.code);return a.$$set=n=>{"collection"in n&&t(0,l=n.collection)},t(2,o=[{code:204,body:"null"},{code:400,body:`
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
            `}]),[l,d,o,r]}class He extends se{constructor(e){super(),ie(this,e,We,Ne,oe,{collection:0})}}function we(a,e,t){const l=a.slice();return l[4]=e[t],l}function Ce(a,e,t){const l=a.slice();return l[4]=e[t],l}function ye(a,e){let t,l=e[4].code+"",d,o,r,n;function m(){return e[3](e[4])}return{key:a,first:null,c(){t=p("button"),d=U(l),o=y(),b(t,"class","tab-item"),K(t,"active",e[1]===e[4].code),this.first=t},m(k,q){v(k,t,q),u(t,d),u(t,o),r||(n=ce(t,"click",m),r=!0)},p(k,q){e=k,q&4&&l!==(l=e[4].code+"")&&F(d,l),q&6&&K(t,"active",e[1]===e[4].code)},d(k){k&&g(t),r=!1,n()}}}function Ee(a,e){let t,l,d,o;return l=new Pe({props:{content:e[4].body}}),{key:a,first:null,c(){t=p("div"),te(l.$$.fragment),d=y(),b(t,"class","tab-item"),K(t,"active",e[1]===e[4].code),this.first=t},m(r,n){v(r,t,n),ee(l,t,null),u(t,d),o=!0},p(r,n){e=r;const m={};n&4&&(m.content=e[4].body),l.$set(m),(!o||n&6)&&K(t,"active",e[1]===e[4].code)},i(r){o||(V(l.$$.fragment,r),o=!0)},o(r){X(l.$$.fragment,r),o=!1},d(r){r&&g(t),x(l)}}}function Le(a){let e,t,l,d,o,r,n,m=a[0].name+"",k,q,Y,H,J,L,G,B,D,O,N,A,P,R=[],j=new Map,T,W,w=[],E=new Map,M,_=z(a[2]);const Z=s=>s[4].code;for(let s=0;s<_.length;s+=1){let h=Ce(a,_,s),S=Z(h);j.set(S,R[s]=ye(S,h))}let c=z(a[2]);const f=s=>s[4].code;for(let s=0;s<c.length;s+=1){let h=we(a,c,s),S=f(h);E.set(S,w[s]=Ee(S,h))}return{c(){e=p("div"),t=p("strong"),t.textContent="POST",l=y(),d=p("div"),o=p("p"),r=U("/api/collections/"),n=p("strong"),k=U(m),q=U("/request-email-change"),Y=y(),H=p("p"),H.innerHTML="Requires <code>Authorization:TOKEN</code>",J=y(),L=p("div"),L.textContent="Body Parameters",G=y(),B=p("table"),B.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr></thead> <tbody><tr><td><div class="inline-flex"><span class="label label-success">Required</span> <span>newEmail</span></div></td> <td><span class="label">String</span></td> <td>The new email address to send the change email request.</td></tr></tbody>',D=y(),O=p("div"),O.textContent="Responses",N=y(),A=p("div"),P=p("div");for(let s=0;s<R.length;s+=1)R[s].c();T=y(),W=p("div");for(let s=0;s<w.length;s+=1)w[s].c();b(t,"class","label label-primary"),b(d,"class","content"),b(H,"class","txt-hint txt-sm txt-right"),b(e,"class","alert alert-success"),b(L,"class","section-title"),b(B,"class","table-compact table-border m-b-base"),b(O,"class","section-title"),b(P,"class","tabs-header compact combined left"),b(W,"class","tabs-content"),b(A,"class","tabs")},m(s,h){v(s,e,h),u(e,t),u(e,l),u(e,d),u(d,o),u(o,r),u(o,n),u(n,k),u(o,q),u(e,Y),u(e,H),v(s,J,h),v(s,L,h),v(s,G,h),v(s,B,h),v(s,D,h),v(s,O,h),v(s,N,h),v(s,A,h),u(A,P);for(let S=0;S<R.length;S+=1)R[S]&&R[S].m(P,null);u(A,T),u(A,W);for(let S=0;S<w.length;S+=1)w[S]&&w[S].m(W,null);M=!0},p(s,[h]){(!M||h&1)&&m!==(m=s[0].name+"")&&F(k,m),h&6&&(_=z(s[2]),R=le(R,h,Z,1,s,_,j,P,Se,ye,null,Ce)),h&6&&(c=z(s[2]),ae(),w=le(w,h,f,1,s,c,E,W,Oe,Ee,null,we),ne())},i(s){if(!M){for(let h=0;h<c.length;h+=1)V(w[h]);M=!0}},o(s){for(let h=0;h<w.length;h+=1)X(w[h]);M=!1},d(s){s&&(g(e),g(J),g(L),g(G),g(B),g(D),g(O),g(N),g(A));for(let h=0;h<R.length;h+=1)R[h].d();for(let h=0;h<w.length;h+=1)w[h].d()}}}function Ue(a,e,t){let{collection:l}=e,d=204,o=[];const r=n=>t(1,d=n.code);return a.$$set=n=>{"collection"in n&&t(0,l=n.collection)},t(2,o=[{code:204,body:"null"},{code:400,body:`
                {
                  "status": 400,
                  "message": "An error occurred while validating the submitted data.",
                  "data": {
                    "newEmail": {
                      "code": "validation_required",
                      "message": "Missing required value."
                    }
                  }
                }
            `},{code:401,body:`
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
            `}]),[l,d,o,r]}class Ie extends se{constructor(e){super(),ie(this,e,Ue,Le,oe,{collection:0})}}function Ae(a,e,t){const l=a.slice();return l[5]=e[t],l[7]=t,l}function Te(a,e,t){const l=a.slice();return l[5]=e[t],l[7]=t,l}function qe(a){let e,t,l,d,o;function r(){return a[4](a[7])}return{c(){e=p("button"),t=p("div"),t.textContent=`${a[5].title}`,l=y(),b(t,"class","txt"),b(e,"class","tab-item"),K(e,"active",a[1]==a[7])},m(n,m){v(n,e,m),u(e,t),u(e,l),d||(o=ce(e,"click",r),d=!0)},p(n,m){a=n,m&2&&K(e,"active",a[1]==a[7])},d(n){n&&g(e),d=!1,o()}}}function Re(a){let e,t,l,d;var o=a[5].component;function r(n,m){return{props:{collection:n[0]}}}return o&&(t=be(o,r(a))),{c(){e=p("div"),t&&te(t.$$.fragment),l=y(),b(e,"class","tab-item"),K(e,"active",a[1]==a[7])},m(n,m){v(n,e,m),t&&ee(t,e,null),u(e,l),d=!0},p(n,m){if(o!==(o=n[5].component)){if(t){ae();const k=t;X(k.$$.fragment,1,0,()=>{x(k,1)}),ne()}o?(t=be(o,r(n)),te(t.$$.fragment),V(t.$$.fragment,1),ee(t,e,l)):t=null}else if(o){const k={};m&1&&(k.collection=n[0]),t.$set(k)}(!d||m&2)&&K(e,"active",n[1]==n[7])},i(n){d||(t&&V(t.$$.fragment,n),d=!0)},o(n){t&&X(t.$$.fragment,n),d=!1},d(n){n&&g(e),t&&x(t)}}}function ze(a){var c,f,s,h,S,re;let e,t,l=a[0].name+"",d,o,r,n,m,k,q,Y=a[0].name+"",H,J,L,G,B,D,O,N,A,P,R,j,T,W;D=new Me({props:{js:`
        import Base from 'base';

        const base = new Base('${a[2]}');

        ...

        await base.collection('${(c=a[0])==null?void 0:c.name}').authWithPassword('test@example.com', '1234567890');

        await base.collection('${(f=a[0])==null?void 0:f.name}').requestEmailChange('new@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(s=a[0])==null?void 0:s.name}').confirmEmailChange(
            'EMAIL_CHANGE_TOKEN',
            'YOUR_PASSWORD',
        );
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${a[2]}');

        ...

        await base.collection('${(h=a[0])==null?void 0:h.name}').authWithPassword('test@example.com', '1234567890');

        await base.collection('${(S=a[0])==null?void 0:S.name}').requestEmailChange('new@example.com');

        ...

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(re=a[0])==null?void 0:re.name}').confirmEmailChange(
          'EMAIL_CHANGE_TOKEN',
          'YOUR_PASSWORD',
        );
    `}});let w=z(a[3]),E=[];for(let i=0;i<w.length;i+=1)E[i]=qe(Te(a,w,i));let M=z(a[3]),_=[];for(let i=0;i<M.length;i+=1)_[i]=Re(Ae(a,M,i));const Z=i=>X(_[i],1,1,()=>{_[i]=null});return{c(){e=p("h3"),t=U("Email change ("),d=U(l),o=U(")"),r=y(),n=p("div"),m=p("p"),k=U("Sends "),q=p("strong"),H=U(Y),J=U(" email change request."),L=y(),G=p("p"),G.textContent=`On successful email change all previously issued auth tokens for the specific record will be
        automatically invalidated.`,B=y(),te(D.$$.fragment),O=y(),N=p("h6"),N.textContent="API details",A=y(),P=p("div"),R=p("div");for(let i=0;i<E.length;i+=1)E[i].c();j=y(),T=p("div");for(let i=0;i<_.length;i+=1)_[i].c();b(e,"class","m-b-sm"),b(n,"class","content txt-lg m-b-sm"),b(N,"class","m-b-xs"),b(R,"class","tabs-header compact"),b(T,"class","tabs-content"),b(P,"class","tabs")},m(i,C){v(i,e,C),u(e,t),u(e,d),u(e,o),v(i,r,C),v(i,n,C),u(n,m),u(m,k),u(m,q),u(q,H),u(m,J),u(n,L),u(n,G),v(i,B,C),ee(D,i,C),v(i,O,C),v(i,N,C),v(i,A,C),v(i,P,C),u(P,R);for(let I=0;I<E.length;I+=1)E[I]&&E[I].m(R,null);u(P,j),u(P,T);for(let I=0;I<_.length;I+=1)_[I]&&_[I].m(T,null);W=!0},p(i,[C]){var de,ue,fe,me,he,pe;(!W||C&1)&&l!==(l=i[0].name+"")&&F(d,l),(!W||C&1)&&Y!==(Y=i[0].name+"")&&F(H,Y);const I={};if(C&5&&(I.js=`
        import Base from 'base';

        const base = new Base('${i[2]}');

        ...

        await base.collection('${(de=i[0])==null?void 0:de.name}').authWithPassword('test@example.com', '1234567890');

        await base.collection('${(ue=i[0])==null?void 0:ue.name}').requestEmailChange('new@example.com');

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(fe=i[0])==null?void 0:fe.name}').confirmEmailChange(
            'EMAIL_CHANGE_TOKEN',
            'YOUR_PASSWORD',
        );
    `),C&5&&(I.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${i[2]}');

        ...

        await base.collection('${(me=i[0])==null?void 0:me.name}').authWithPassword('test@example.com', '1234567890');

        await base.collection('${(he=i[0])==null?void 0:he.name}').requestEmailChange('new@example.com');

        ...

        // ---
        // (optional) in your custom confirmation page:
        // ---

        // note: after this call all previously issued auth tokens are invalidated
        await base.collection('${(pe=i[0])==null?void 0:pe.name}').confirmEmailChange(
          'EMAIL_CHANGE_TOKEN',
          'YOUR_PASSWORD',
        );
    `),D.$set(I),C&10){w=z(i[3]);let $;for($=0;$<w.length;$+=1){const Q=Te(i,w,$);E[$]?E[$].p(Q,C):(E[$]=qe(Q),E[$].c(),E[$].m(R,null))}for(;$<E.length;$+=1)E[$].d(1);E.length=w.length}if(C&11){M=z(i[3]);let $;for($=0;$<M.length;$+=1){const Q=Ae(i,M,$);_[$]?(_[$].p(Q,C),V(_[$],1)):(_[$]=Re(Q),_[$].c(),V(_[$],1),_[$].m(T,null))}for(ae(),$=M.length;$<_.length;$+=1)Z($);ne()}},i(i){if(!W){V(D.$$.fragment,i);for(let C=0;C<M.length;C+=1)V(_[C]);W=!0}},o(i){X(D.$$.fragment,i),_=_.filter(Boolean);for(let C=0;C<_.length;C+=1)X(_[C]);W=!1},d(i){i&&(g(e),g(r),g(n),g(B),g(O),g(N),g(A),g(P)),x(D,i),_e(E,i),_e(_,i)}}}function Ke(a,e,t){let l,{collection:d}=e;const o=[{title:"Request email change",component:Ie},{title:"Confirm email change",component:He}];let r=0;const n=m=>t(1,r=m);return a.$$set=m=>{"collection"in m&&t(0,d=m.collection)},t(2,l=Be.getApiExampleUrl(De.baseURL)),[d,r,l,o,n]}class Ge extends se{constructor(e){super(),ie(this,e,Ke,ze,oe,{collection:0})}}export{Ge as default};
