/*
   Copyright The Soci Snapshotter Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package commands

import (
	"errors"
	"fmt"
	"os"

	"github.com/awslabs/soci-snapshotter/cmd/soci/commands/internal"
	"github.com/awslabs/soci-snapshotter/config"
	"github.com/awslabs/soci-snapshotter/soci"
	"github.com/awslabs/soci-snapshotter/soci/store"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/images"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/urfave/cli"
)

// ConvertCommand converts an image into a SOCI enabled image.
// The new image is added to the containerd content store and can
// be pushed and deployed like a normal image.
var ConvertCommand = cli.Command{
	Name:      "convert",
	Usage:     "convert an OCI image to a SOCI enabled image",
	ArgsUsage: "[flags] <image_ref> <dest_ref>",
	Flags: append(
		internal.PlatformFlags,
		cli.Int64Flag{
			Name:  spanSizeFlag,
			Usage: "Span size that soci index uses to segment layer data. Default is 4 MiB",
			Value: 1 << 22,
		},
		cli.Int64Flag{
			Name:  minLayerSizeFlag,
			Usage: "Minimum layer size to build zTOC for. Smaller layers won't have zTOC and not lazy pulled. Default is 10 MiB.",
			Value: 10 << 20,
		},
		cli.StringSliceFlag{
			Name:  optimizationFlag,
			Usage: fmt.Sprintf("(Experimental) Enable optional optimizations. Valid values are %v", soci.Optimizations),
		},
	),
	Action: func(cliContext *cli.Context) error {
		srcRef := cliContext.Args().Get(0)
		if srcRef == "" {
			return errors.New("source image needs to be specified")
		}

		dstRef := cliContext.Args().Get(1)
		if dstRef == "" {
			return errors.New("destination image needs to be specified")
		}

		var optimizations []soci.Optimization
		for _, o := range cliContext.StringSlice(optimizationFlag) {
			optimization, err := soci.ParseOptimization(o)
			if err != nil {
				return err
			}
			optimizations = append(optimizations, optimization)
		}

		client, ctx, cancel, err := internal.NewClient(cliContext)
		if err != nil {
			return err
		}
		defer cancel()

		cs := client.ContentStore()
		is := client.ImageService()
		srcImg, err := is.Get(ctx, srcRef)
		if err != nil {
			return err
		}
		spanSize := cliContext.Int64(spanSizeFlag)
		minLayerSize := cliContext.Int64(minLayerSizeFlag)
		// Creating the snapshotter's root path first if it does not exist, since this ensures, that
		// it has the limited permission set as drwx--x--x.
		// The subsequent oci.New creates a root path dir with too broad permission set.
		if _, err := os.Stat(config.SociSnapshotterRootPath); os.IsNotExist(err) {
			if err = os.Mkdir(config.SociSnapshotterRootPath, 0711); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		blobStore, err := store.NewContentStore(internal.ContentStoreOptions(cliContext)...)
		if err != nil {
			return err
		}

		artifactsDb, err := soci.NewDB(soci.ArtifactsDbPath())
		if err != nil {
			return err
		}

		builderOpts := []soci.BuilderOption{
			soci.WithMinLayerSize(minLayerSize),
			soci.WithSpanSize(spanSize),
			soci.WithBuildToolIdentifier(buildToolIdentifier),
			soci.WithOptimizations(optimizations),
			soci.WithArtifactsDb(artifactsDb),
		}

		builder, err := soci.NewIndexBuilder(cs, blobStore, builderOpts...)

		if err != nil {
			return err
		}

		batchCtx, done, err := blobStore.BatchOpen(ctx)
		if err != nil {
			return err
		}
		defer done(ctx)

		var platforms []v1.Platform
		explicitPlatforms := cliContext.StringSlice(internal.PlatformFlagKey)
		if len(explicitPlatforms) > 0 {
			platforms, err = internal.GetPlatforms(ctx, cliContext, srcImg, cs)
		} else {
			platforms, err = images.Platforms(ctx, cs, srcImg.Target)
		}
		if err != nil {
			return err
		}

		desc, err := builder.Convert(batchCtx, srcImg, soci.ConvertWithPlatforms(platforms...), soci.ConvertWithNoGarbageCollectionLabels())
		if err != nil {
			return err
		}

		im := images.Image{
			Name:   dstRef,
			Target: *desc,
		}
		img, err := is.Get(ctx, dstRef)
		if err != nil {
			if !errors.Is(err, errdefs.ErrNotFound) {
				return err
			}
			_, err = is.Create(ctx, im)
			return err
		}
		img.Target = *desc
		_, err = is.Update(ctx, img)

		return err
	},
}
