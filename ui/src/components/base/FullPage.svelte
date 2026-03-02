<script>
    import PageWrapper from "@/components/base/PageWrapper.svelte";
    import { appName, logoUrl } from "@/stores/app";

    export let nobranding = false;
</script>

<PageWrapper class="full-page" center>
    <div class="wrapper wrapper-sm m-b-xl panel-wrapper">
        {#if !nobranding}
            <div class="block txt-center m-b-lg">
                <figure class="logo">
                    <img
                        src={$logoUrl || `${import.meta.env.BASE_URL}images/logo.svg`}
                        alt={$appName || "Hanzo Base"}
                        width="40"
                        height="40"
                    />
                    <span class="txt"><strong>{$appName || "Hanzo Base"}</strong></span>
                </figure>
            </div>
            <div class="clearfix" />
        {/if}

        <slot />
    </div>
</PageWrapper>

<style>
    .panel-wrapper {
        animation: slideIn 200ms;
    }
</style>
