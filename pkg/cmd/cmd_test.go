package cmd

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
)

func TestOptions_Run(t *testing.T) {
	const testNamespace = "test"

	testPodList, _, _ := cmdtesting.TestData()
	testPodList.Items[0].Status.Phase = corev1.PodFailed  // name="foo"
	testPodList.Items[1].Status.Phase = corev1.PodRunning // name="bar"
	testPods := podListToPods(testPodList)

	tf := cmdtesting.NewTestFactory().WithNamespace(testNamespace)
	defer tf.Cleanup()

	codec := scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)

	tf.UnstructuredClient = &fake.RESTClient{
		NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
		Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			switch p, m := req.URL.Path, req.Method; {
			case p == fmt.Sprintf("/namespaces/%s/pods", testNamespace) && m == http.MethodGet:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     cmdtesting.DefaultHeader(),
					Body:       cmdtesting.ObjBody(codec, testPodList),
				}, nil

			case p == fmt.Sprintf("/namespaces/%s/pods/foo", testNamespace) && m == http.MethodDelete:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     cmdtesting.DefaultHeader(),
					Body:       cmdtesting.ObjBody(codec, &testPodList.Items[0]),
				}, nil

			default:
				t.Errorf("unexpected request: %#v\n%#v", req.URL, req)
			}

			return nil, nil
		}),
	}

	type fields struct {
		printFlags     *genericclioptions.PrintFlags
		dryRunStrategy cmdutil.DryRunStrategy
		determiner     *determiner
		result         *resource.Result
		IOStreams      genericclioptions.IOStreams
	}

	tests := []struct {
		name    string
		fields  fields
		wantOut string
		wantErr bool
	}{
		{
			name: "delete pod that should be deleted",
			fields: fields{
				determiner: &determiner{
					pods: testPods,
				},
			},
			wantOut: "pod/foo deleted\n",
			wantErr: false,
		},
		{
			name: "not delete pod that should be deleted when dry-run is set",
			fields: fields{
				determiner: &determiner{
					pods: testPods,
				},
				dryRunStrategy: cmdutil.DryRunClient,
			},
			wantOut: "pod/foo deleted (dry run)\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			streams, _, out, _ := genericclioptions.NewTestIOStreams()

			o := &Options{
				printFlags:     genericclioptions.NewPrintFlags("deleted").WithTypeSetter(scheme.Scheme),
				namespace:      testNamespace,
				chunkSize:      10,
				dryRunStrategy: tt.fields.dryRunStrategy,
				IOStreams:      streams,
			}

			o.printFlags = cmdutil.PrintFlagsWithDryRunStrategy(o.printFlags, o.dryRunStrategy)
			printer, err := o.printFlags.ToPrinter()
			if err != nil {
				t.Errorf("failed to build printer: %v\n", err)
				return
			}
			o.printObj = func(obj runtime.Object) error {
				return printer.PrintObj(obj, o.Out)
			}

			o.result = tf.
				NewBuilder().
				Unstructured().
				ContinueOnError().
				NamespaceParam(o.namespace).
				DefaultNamespace().
				AllNamespaces(o.allNamespaces).
				ResourceTypeOrNameArgs(false, "pods").
				RequestChunksOf(o.chunkSize).
				SelectAllParam(true).
				Flatten().
				Do()

			if err := o.result.Err(); err != nil {
				t.Errorf("failed to fetch resources: %v\n", err)
				return
			}

			if err := o.Run(tf); (err != nil) != tt.wantErr {
				t.Errorf("Options.Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.wantOut, out.String()); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}