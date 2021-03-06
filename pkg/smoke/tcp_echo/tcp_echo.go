package tcp_echo

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/skupperproject/skupper-cli/pkg/smoke"
)

type SmokeTestRunner struct {
	smoke.SmokeTestRunnerBase
}

func int32Ptr(i int32) *int32 { return &i }

const minute time.Duration = 60

var deployment *appsv1.Deployment = &appsv1.Deployment{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: "tcp-go-echo",
	},
	Spec: appsv1.DeploymentSpec{
		Replicas: int32Ptr(1),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"application": "tcp-go-echo"},
		},
		Template: apiv1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"application": "tcp-go-echo",
				},
			},
			Spec: apiv1.PodSpec{
				Containers: []apiv1.Container{
					{
						Name:            "tcp-go-echo",
						Image:           "quay.io/skupper/tcp-go-echo",
						ImagePullPolicy: apiv1.PullIfNotPresent,
						Ports: []apiv1.ContainerPort{
							{
								Name:          "http",
								Protocol:      apiv1.ProtocolTCP,
								ContainerPort: 80,
							},
						},
					},
				},
			},
		},
	},
}

func sendReceive(servAddr string) {
	strEcho := "Halo"
	//servAddr := ip + ":9090"
	tcpAddr, err := net.ResolveTCPAddr("tcp", servAddr)
	if err != nil {
		log.Panicln("ResolveTCPAddr failed:", err.Error())
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		log.Panicln("Dial failed:", err.Error())
	}
	_, err = conn.Write([]byte(strEcho))
	if err != nil {
		log.Panicln("Write to server failed:", err.Error())
	}

	reply := make([]byte, 1024)

	_, err = conn.Read(reply)
	if err != nil {
		log.Panicln("Write to server failed:", err.Error())
	}
	conn.Close()

	log.Println("Sent to server = ", strEcho)
	log.Println("Reply from server = ", string(reply))

	if !strings.Contains(string(reply), strings.ToUpper(strEcho)) {
		log.Panicf("Response from server different that expected: %s", string(reply))
	}
}

func (r *SmokeTestRunner) RunTests() {
	var publicService *apiv1.Service
	var privateService *apiv1.Service

	//TODO deduplicate
	r.Pub1Cluster.KubectlExec("get svc")
	r.Priv1Cluster.KubectlExec("get svc")

	publicService = r.Pub1Cluster.GetService("tcp-go-echo", minute)
	privateService = r.Priv1Cluster.GetService("tcp-go-echo", minute)

	fmt.Printf("Public service ClusterIp = %q\n", publicService.Spec.ClusterIP)
	fmt.Printf("Private service ClusterIp = %q\n", privateService.Spec.ClusterIP)

	r.Pub1Cluster.KubectlExecAsync("port-forward service/tcp-go-echo 9090:9090")
	r.Priv1Cluster.KubectlExecAsync("port-forward service/tcp-go-echo 9091:9090")

	time.Sleep(2 * time.Second) //give time to port forwarding to start

	//sendReceive(publicService.Spec.ClusterIP + ":9090")
	//sendReceive(privateService.Spec.ClusterIP + ":9090")
	sendReceive("127.0.0.1:9090")
	sendReceive("127.0.0.1:9091")
}

func (r *SmokeTestRunner) Setup() {
	r.Pub1Cluster.CreateNamespace()
	r.Priv1Cluster.CreateNamespace()

	publicDeploymentsClient := r.Pub1Cluster.Clientset.AppsV1().Deployments(r.Pub1Cluster.Namespace)

	fmt.Println("Creating deployment...")
	result, err := publicDeploymentsClient.Create(deployment)
	if err != nil {
		panic(err)
	}
	log.Println("================")
	//log.Println(err.Error())

	fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())

	fmt.Printf("Listing deployments in namespace %q:\n", "public")
	list, err := publicDeploymentsClient.List(metav1.ListOptions{})
	if err != nil {
		log.Panic(err.Error())
	}
	for _, d := range list.Items {
		fmt.Printf(" * %s (%d replicas)\n", d.Name, *d.Spec.Replicas)
	}

	r.Pub1Cluster.SkupperExec("init --cluster-local")
	r.Pub1Cluster.SkupperExec("expose --port 9090 deployment tcp-go-echo")
	r.Pub1Cluster.SkupperExec("connection-token /tmp/public_secret.yaml")

	r.Priv1Cluster.SkupperExec("init --cluster-local")
	r.Priv1Cluster.SkupperExec("connect /tmp/public_secret.yaml")

	r.Pub1Cluster.GetService("tcp-go-echo", 10*minute)
	r.Priv1Cluster.GetService("tcp-go-echo", 3*minute)
}

func (r *SmokeTestRunner) TearDown() {
	//since this is going to run in a spawned ci vm (then destroyed) probably
	//tearDown is not so important
	publicDeploymentsClient := r.Pub1Cluster.Clientset.AppsV1().Deployments(r.Pub1Cluster.Namespace)
	fmt.Println("Deleting deployment...")
	deletePolicy := metav1.DeletePropagationForeground
	if err := publicDeploymentsClient.Delete("tcp-go-echo", &metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}); err != nil {
		log.Panic(err.Error())
	}
	fmt.Println("Deleted deployment.")

	r.Pub1Cluster.SkupperExec("delete")
	r.Priv1Cluster.SkupperExec("delete")

	//r.deleteNamespaces()??
	r.Pub1Cluster.DeleteNamespace()
	r.Priv1Cluster.DeleteNamespace()
}

func (r *SmokeTestRunner) Run() {
	defer r.TearDown()
	r.Setup()
	r.RunTests()
}
